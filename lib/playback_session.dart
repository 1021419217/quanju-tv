import 'dart:async';
import 'dart:io';
import 'dart:ui' show Size;

import 'package:flutter/foundation.dart';
import 'package:media_kit/media_kit.dart';

import 'core_bridge.dart';
import 'danmaku_controller.dart';
import 'diary_service.dart';
import 'local_store.dart';
import 'models.dart';
import 'playback_loader.dart';
import 'playback_preferences.dart';
import 'playback_preloader.dart';
import 'playback_recovery.dart';

Size? videoDisplaySize(VideoParams parameters) {
  final width = (parameters.dw ?? 0) > 0 ? parameters.dw! : parameters.w ?? 0;
  final height = (parameters.dh ?? 0) > 0 ? parameters.dh! : parameters.h ?? 0;
  if (width <= 0 || height <= 0) return null;
  final rotation = (parameters.rotate ?? 0) % 180;
  return rotation == 90
      ? Size(height.toDouble(), width.toDouble())
      : Size(width.toDouble(), height.toDouble());
}

String _diagnostic(Object error) {
  final text = error.toString().replaceAll(
    RegExp(r'[a-z]+://\S+', caseSensitive: false),
    '[地址]',
  );
  return text.length > 300 ? text.substring(0, 300) : text;
}

class PlaybackSession extends ChangeNotifier {
  PlaybackSession({
    required this.repository,
    required this.store,
    required this.detail,
    required this._player,
    this._standby,
    required int index,
    double position = 0,
    bool autoplay = true,
    bool preferLocal = true,
    bool localOnly = false,
  }) : _engaged = autoplay,
       _index = index,
       _preferLocal = preferLocal,
       _localOnly = localOnly,
       _epoch = store.profileEpoch {
    final preferences = store.playbackPreferences;
    _speed = preferences.speed;
    _boostSpeed = preferences.boostSpeed;
    _quality = preferences.quality;
    _danmakuEnabled = preferences.danmaku;
    _adBlock = preferences.adBlock;
    _loader = PlaybackLoader(repository);
    _preloader = PlaybackPreloader(repository)..addListener(_prime);
    danmaku = DanmakuController(repository)..setEnabled(_danmakuEnabled);
    store.addListener(_accessChanged);
    _bind();
    _saveTimer = Timer.periodic(
      const Duration(seconds: 5),
      (_) => saveProgress(),
    );
    _healthTimer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (_closed || !_acceptErrors || _error != null) return;
      if (_loading) {
        // 20 秒还没起播：中转侧「试旧地址 6s + 重解析 8s」早该有结论了，
        // 这时还卡着只可能是链路僵死，主动走一次恢复（换线 → 重解析）。
        final since = _loadingSince;
        if (since != null &&
            DateTime.now().difference(since) >=
                const Duration(seconds: 20)) {
          _loadingSince = DateTime.now();
          unawaited(_recover('startup_timeout'));
        }
        return;
      }
      if (_health.stalled(
        position: player.state.position,
        playing: player.state.playing && _playIntent,
        foreground: _foreground,
        now: DateTime.now(),
      )) {
        unawaited(_recover('position_stalled_20s'));
      }
    });
    unawaited(play(index, position: position, playWhenReady: autoplay));
  }

  void _bind() {
    for (final subscription in _subscriptions) {
      unawaited(subscription.cancel());
    }
    _subscriptions.clear();
    final size = videoDisplaySize(player.state.videoParams);
    if (size != null) {
      _aspectRatio = size.width / size.height;
      _aspectKnown = true;
    }
    _buffering(player.state.buffering);
    final stream = player.stream;
    _subscriptions.addAll([
      stream.error.listen(_playerError),
      stream.completed.listen(_completed),
      stream.position.listen((position) {
        if (!_closed && _openedIndex == _index && position > Duration.zero) {
          _resumePosition = position.inMilliseconds / 1000;
        }
        _sync();
      }),
      stream.playing.listen((playing) {
        if (!playing) endBoost();
        _sync();
      }),
      stream.buffering.listen((buffering) {
        _buffering(buffering);
        _sync();
      }),
      for (final changes in [stream.duration, stream.buffer, stream.rate])
        changes.listen((_) => _sync()),
      stream.videoParams.listen((parameters) {
        final size = videoDisplaySize(parameters);
        if (size == null || _closed) return;
        _aspectRatio = size.width / size.height;
        _aspectKnown = true;
        _notify();
      }),
    ]);
  }

  final AppRepository repository;
  final LocalStore store;
  final DramaDetail detail;
  Player _player;
  Player? _standby;
  late final DanmakuController danmaku;
  late final PlaybackLoader _loader;
  late final PlaybackPreloader _preloader;
  final _recovery = PlaybackRecovery();
  final _health = PlaybackHealth();
  final int _epoch;
  // 下载好的剧集优先读本地文件：既省流量，也让「源站挂了/断网」时下载内容
  // 仍然能看。localOnly 是「已下载」页点播放时用的，只认本地，缺文件直接
  // 报错，不会偷偷回落到在线取流。
  final bool _preferLocal;
  final bool _localOnly;
  final List<StreamSubscription<dynamic>> _subscriptions = [];
  Timer? _saveTimer;
  Timer? _healthTimer;
  Timer? _errorTimer;
  Timer? _boostTimer;
  Timer? _stallTimer;
  Future<void> _operations = Future<void>.value();
  Future<void> _rates = Future<void>.value();
  int _index;
  int _openedIndex = -1;
  int _generation = 0;
  int _seekSequence = 0;
  bool _loading = true;
  // 起播看门狗：_loading 为真时健康检查整段跳过，一旦上游卡死，
  // 播放器就只能干等它自己的超时（用户看到的是「一直转圈最后播不了」）。
  DateTime? _loadingSince = DateTime.now();
  bool _online = false;
  bool _localFailure = false;
  bool _closed = false;
  bool _acceptErrors = false;
  bool _foreground = true;
  bool _playIntent = true;
  bool _pendingError = false;
  bool _boosting = false;
  bool _boostLocked = false;
  bool _engaged;
  bool _staleAudio = false;
  bool _stalled = false;
  String _loadingMessage = '正在准备播放';
  String? _error;
  String? _saveWarning;
  String? _savedProgressKey;
  PlaybackPlan? _plan;
  PlaybackPlan? _standbyPlan;
  int _standbyIndex = -1;
  bool _standbyOpen = false;
  DateTime _standbyAt = DateTime(2000);
  double _openedAt = 0;
  double _speed = 1;
  double _boostSpeed = 2;
  int _quality = 0;
  int _route = 0;
  bool _danmakuEnabled = true;
  bool _adBlock = true;
  double _aspectRatio = 9 / 16;
  bool _aspectKnown = false;
  double _resumePosition = 0;
  double _unmutedVolume = 100;

  Player get player => _player;

  Player? get standby => _standbyOpen ? _standby : null;
  int get standbyIndex => _standbyOpen ? _standbyIndex : -1;
  int get index => _index;
  Episode get episode => detail.episodes[_index];
  bool get loading => _loading;
  String get loadingMessage => _loadingMessage;
  String? get error => _error;
  bool get localFailure => _localFailure;
  String? get saveWarning => _saveWarning;
  bool get ready => !_loading && _error == null;

  bool get stalled => _stalled;

  bool get paused => !_playIntent;
  bool get boosting => _boosting;
  bool get boostLocked => _boostLocked;
  double get aspectRatio => _aspectRatio;
  bool get aspectKnown => _aspectKnown;
  double get speed => _speed;

  /// 长按倍速的当前档位（2 / 2.5 / 3）。
  double get boostSpeed => _boostSpeed;
  int get quality => _quality;
  bool get local => _plan?.local == true;
  List<int> get qualities => _plan?.qualities ?? const [];
  bool get danmakuSupported => detail.drama.source == SourceSite.hongguo.id;
  bool get danmakuEnabled => _danmakuEnabled;

  /// 当前播放线路（1 起）与可用线路总数。本地播放或单线路时总数为 1。
  int get routeIndex => _plan?.routeIndex ?? 0;
  int get routeCount => _plan?.routeCount ?? 1;
  bool get canSwitchRoute => _plan != null && _plan!.routeCount > 1;

  /// 已选中的线路序号（0 起）。切集、换清晰度都沿用它，只有这一集没有这条
  /// 线路时才会改用别的线路，避免每集都跳回线路 1。
  int get preferredRoute => _route;

  /// 投屏用的播放地址：指向核心的本机流服务，投屏时要把主机换成手机内网地址。
  String get streamUrl => _plan?.url ?? '';

  /// 站源给出的原始播放地址。非空时投屏直接把它交给电视，电视自己上网拉流，
  /// 手机退出 App 也不影响播放；为空说明这条线路只能经手机转发。
  String get castDirectUrl => _plan?.directUrl ?? '';

  /// 投屏到电视时显示的标题：剧名 + 当前集。
  String get castTitle {
    final name = detail.drama.title;
    final label = episode.title;
    return label.isEmpty ? name : '$name $label';
  }

  /// 去广告：解析播放地址时尝试跳过源站片头广告。
  bool get adBlock => _adBlock;
  bool get hasPrevious => _index > 0;
  bool get hasNext => _index + 1 < detail.episodes.length;
  String get qualityLabel {
    final plan = _plan;
    if (plan?.local == true) return '本地原画';
    if (_quality > 0) return '${_quality}P';
    return plan != null && plan.quality > 0 ? '自动（${plan.quality}P）' : '自动';
  }

  double get currentPosition =>
      _openedIndex == _index && player.state.position > Duration.zero
      ? player.state.position.inMilliseconds / 1000
      : _resumePosition;

  bool get _usable => !_closed && store.profileEpoch == _epoch;
  bool _current(int ticket) => _usable && ticket == _generation;

  PlaybackPreferences get _preferences => store.playbackPreferences.copyWith(
    speed: _speed,
    quality: _quality,
    danmaku: _danmakuEnabled,
    adBlock: _adBlock,
    autoAdvance: true,
    preload: true,
  );

  double get _rate => _boosting ? _boostSpeed : _speed;

  void _buffering(bool buffering) {
    _stallTimer?.cancel();
    if (buffering) {
      _stallTimer = Timer(const Duration(milliseconds: 500), () {
        if (_closed || !player.state.buffering) return;
        _stalled = true;
        _notify();
      });
    } else if (_stalled) {
      _stalled = false;
      _notify();
    }
  }

  void _notify() {
    if (!_closed) notifyListeners();
  }

  void _accessChanged() {
    if (_closed) return;
    if (store.profileEpoch != _epoch || store.locked) {
      _preloader.clear();
      _dropStandby();
      _playIntent = false;
      endBoost();
      unawaited(player.pause());
    }
    if (store.profileEpoch != _epoch ||
        store.locked ||
        !store.allowsSource(SourceSite.hongguo.id)) {
      danmaku.setPlan(null);
    }
  }

  void _sync() {
    _syncDanmaku();
    _syncPreload();
  }

  void _syncDanmaku() {
    if (_closed) return;
    final state = player.state;
    danmaku.update(
      position: state.position,
      duration: state.duration,
      speed: state.rate,
      playing: state.playing && _playIntent && !state.completed,
      buffering: state.buffering,
      foreground: _foreground,
      available:
          ready &&
          _openedIndex == _index &&
          _usable &&
          !store.locked &&
          store.allowsSource(SourceSite.hongguo.id),
    );
  }

  void _syncPreload() {
    if (_closed) return;
    if (!_foreground ||
        _plan?.local == true ||
        !ready ||
        _openedIndex != _index ||
        !_usable ||
        !_engaged ||
        store.locked ||
        !hasNext) {
      _preloader.clear();
      return;
    }
    final state = player.state;
    final duration = state.duration.inMilliseconds;
    final position = state.position.inMilliseconds;
    final ahead = state.buffer.inMilliseconds - position;
    if (_standby != null) {
      if (_standbyOpen &&
          DateTime.now().difference(_standbyAt) > const Duration(minutes: 8)) {
        _dropStandby();
      }
      if (_standbyIndex >= 0 || duration <= 0 || ahead < 3000) return;
    } else {
      if (!state.playing || state.buffering || !_playIntent) {
        _preloader.pause();
        return;
      }
      if (duration <= 0 ||
          position < 2000 ||
          (position < duration ~/ 2 && duration - position > 45000) ||
          ahead < 5000) {
        return;
      }
    }
    _preloader.prepare(
      detail.drama,
      detail.episodes[_index + 1],
      quality: _quality,
      online: _online,
      adBlock: _adBlock,
      route: _route,
    );
  }

  void _playerError(String error) {
    if (_closed || !_acceptErrors || error.trim().isEmpty) return;
    DiaryService.add('[Play] 播放器报错：${_diagnostic(error)}');
    _queueRecovery('player_error');
  }

  void _completed(bool completed) {
    if (!completed || _loading || _closed || !_acceptErrors || _error != null) {
      return;
    }
    final duration = player.state.duration;
    if (duration <= Duration.zero ||
        player.state.position < duration - const Duration(seconds: 2)) {
      _queueRecovery('early_completion');
    } else if (_foreground && hasNext) {
      unawaited(play(_index + 1));
    } else {
      _playIntent = false;
      endBoost();
      unawaited(player.pause());
      unawaited(saveProgress());
    }
  }

  void _queueRecovery(String reason) {
    if (_closed || !_acceptErrors || _error != null) return;
    _pendingError = true;
    if (!_foreground || (_errorTimer?.isActive ?? false)) return;
    final ticket = _generation;
    final position = player.state.position;
    _errorTimer = Timer(const Duration(milliseconds: 900), () {
      if (!_current(ticket) || !_foreground || !_acceptErrors) return;
      _pendingError = false;
      final state = player.state;
      if (state.playing &&
          !state.buffering &&
          (state.width ?? 0) > 0 &&
          state.position > position + const Duration(milliseconds: 300)) {
        return;
      }
      unawaited(_recover(reason));
    });
  }

  Future<void> _recover(String reason) async {
    final current = _plan;
    if (_closed ||
        !_acceptErrors ||
        !_foreground ||
        current == null ||
        _error != null) {
      return;
    }
    _acceptErrors = false;
    danmaku.setPlan(null);
    _errorTimer?.cancel();
    _pendingError = false;
    final position = currentPosition;
    final action = current.local
        ? PlaybackRecoveryAction.stop
        : _recovery.next(current);
    DiaryService.add(
      '[Play] 自动恢复 原因=$reason 处理=${action.name} 线路=${current.routeIndex + 1}/${current.routeCount}',
    );
    if (action != PlaybackRecoveryAction.stop) {
      await play(_index, position: position, recovery: action);
      return;
    }
    _resumePosition = position;
    final ticket = _generation;
    try {
      await _serialize(() async {
        if (!_current(ticket)) return;
        try {
          await saveProgress();
          _openedIndex = -1;
          await player.stop();
        } finally {
          await _release(current.session);
        }
      });
    } catch (_) {}
    if (!_current(ticket)) return;
    _loading = false;
    _loadingSince = null;
    _localFailure = current.local;
    var issue = '';
    if (!current.local) {
      try {
        await DiaryService.refreshNativeLogs();
        final detail = DiaryService.lastStreamIssue();
        if (detail != null && detail.isNotEmpty) issue = '\n诊断：$detail';
      } catch (_) {
        // 读日志失败不影响错误提示本身。
      }
      if (!_current(ticket)) return;
    }
    _error = current.local
        ? '本地视频读取失败，请重试或重新下载；也可以改为在线播放。'
        : '自动恢复未成功，请检查网络后重试，也可换一集或选择其他清晰度。$issue';
    _notify();
  }

  Future<void> _release(String session) async {
    if (session.isEmpty) return;
    await repository.release(session).catchError((Object _) {});
  }

  Future<void> _serialize(Future<void> Function() operation) {
    final next = _operations.catchError((Object _) {}).then((_) => operation());
    _operations = next;
    return next;
  }

  /// 先试本地（已下载到本机的分集），没有再回落到在线取流。
  /// 「已下载」页进来时 _localOnly=true，缺文件就明确报错，不再退回在线，
  /// 免得用户以为看的是下载好的那一份。
  Future<PlaybackPlan?> _loadPreferLocal(int preferred) async {
    try {
      final local = await repository.localPlayback(
        detail.drama,
        detail.episodes[index],
      );
      if (local != null && local.local) {
        return _loader.use(local);
      }
    } catch (error) {
      if (_localOnly) rethrow;
      DiaryService.add('[Play] 本地文件不可用，回落在在线取流：${_diagnostic(error)}');
    }
    if (_localOnly) {
      throw AppFailure('本地视频不可用，请重新下载或选择在线播放。', code: 'local_media');
    }
    return _loader.load(
      detail.drama,
      detail.episodes[index],
      quality: _quality,
      online: _online,
      adBlock: _adBlock,
      route: preferred,
    );
  }

  Future<void> _configure(Player target, PlaybackPlan plan) async {
    final platform = target.platform;
    if (platform is! NativePlayer) return;
    final hwdec = switch (store.playbackPreferences.televisionDecoder) {
      TelevisionDecoder.software => 'no',
      TelevisionDecoder.hardware when Platform.isAndroid => 'mediacodec-copy',
      TelevisionDecoder.hardware when Platform.isIOS => 'videotoolbox-copy',
      _ when Platform.isWindows => 'd3d11va-copy',
      _ when Platform.isAndroid => 'auto-safe',
      _ => null,
    };
    if (hwdec != null) await platform.setProperty('hwdec', hwdec);
    if (Platform.isAndroid) {
      await platform.setProperty('hwdec-codecs', 'all');
      await platform.setProperty('vd-lavc-skiploopfilter', 'nonkey');
      await platform.setProperty('vd-lavc-threads', '4');
      await platform.setProperty('vd-lavc-fast', 'yes');
      await platform.setProperty('video-sync', 'audio');
      await platform.setProperty('framedrop', 'vo');
    }
    await platform.setProperty(
      'demuxer-lavf-o',
      [
        'seg_max_retry=3',
        'strict=experimental',
        'allowed_extensions=ALL',
        plan.local
            ? 'protocol_whitelist=[file,crypto,data]'
            : 'protocol_whitelist=[http,https,tcp,tls,crypto,data,file]',
        if (plan.decryptionKey.isNotEmpty)
          'decryption_key=${plan.decryptionKey}',
      ].join(','),
    );
    await platform.setProperty('network-timeout', '20');
    if (!plan.local) {
      // 在线播放再兜一层：站方 CDN 掐断长连接时，ffmpeg 自己按 Range 重连，
      // 播放器不用把整条流交回给上层去做「自动恢复」。中转那边也有续传，
      // 两层都开着才能覆盖「中转先断」和「播放器先断」两种情况。
      //
      // 这里只能放「纯数值」选项：mpv 的 stream-lavf-o 是按逗号切分的
      // key=value 列表，像 reconnect_on_http_error=403,404 这种值里带逗号的
      // 选项会被切成两段，整组参数都可能失效。
      await platform.setProperty(
        'stream-lavf-o',
        ['reconnect=1', 'reconnect_streamed=1', 'reconnect_delay_max=5'].join(','),
      );
    }
  }

  Future<void> play(
    int index, {
    double position = 0,
    PlaybackRecoveryAction? recovery,
    bool playWhenReady = true,
    int? switchRoute,
  }) async {
    if (!_usable || index < 0 || index >= detail.episodes.length) return;
    endBoost();
    _staleAudio = false;
    if (index != _index) _online = false;
    if (switchRoute == null &&
        recovery == null &&
        position == 0 &&
        _standbyOpen &&
        _standbyIndex == index &&
        !_online) {
      return _swap(index, playWhenReady);
    }
    _dropStandby();
    final warmed = recovery == null &&
            switchRoute == null
        ? _preloader.take(
            detail.drama,
            detail.episodes[index],
            quality: _quality,
            online: _online,
            adBlock: _adBlock,
            route: _route,
          )
        : null;
    _preloader.clear();
    // 切集、换清晰度都沿用玩家选中的线路；手动切线路时以新线路为准。
    final preferred = switchRoute ?? _route;
    final ticket = ++_generation;
    _seekSequence++;
    danmaku.setPlan(null);
    _acceptErrors = false;
    _pendingError = false;
    _errorTimer?.cancel();
    _health.reset();
    if (recovery == null) {
      _recovery.reset();
      _playIntent = playWhenReady;
    }
    _resumePosition = position;
    _openedAt = position;
    _index = index;
    _loading = true;
    _loadingSince = DateTime.now();
    _error = null;
    _localFailure = false;
    _loadingMessage = switch (recovery) {
      PlaybackRecoveryAction.alternative => '正在切换备用线路',
      PlaybackRecoveryAction.refresh => '正在重新获取播放地址',
      _ => switchRoute != null ? '正在切换线路' : '正在准备播放',
    };
    _notify();
    PlaybackPlan? prepared;
    PlaybackPlan? retained;
    var installed = false;
    try {
      await _serialize(() async {
        if (!_current(ticket)) return;
        await saveProgress();
        if (!_current(ticket)) return;
        _openedIndex = -1;
        await player.stop();
        final previous = _plan;
        _plan = null;
        // 手动切线路与自动换备线都复用旧会话（selectRoute/fallback），
        // 其余情况释放旧会话。
        if (recovery == PlaybackRecoveryAction.alternative ||
            switchRoute != null) {
          retained = previous;
        } else if (previous != null) {
          await _release(previous.session);
        }
      });
      if (!_current(ticket)) return;
      final retainedPlan = retained;
      prepared = switchRoute != null && retainedPlan != null
          ? await _loader.selectRoute(retainedPlan, switchRoute)
          : retainedPlan != null
          ? await _loader.fallback(retainedPlan)
          : warmed != null
          ? await _loader.use(warmed)
          : _preferLocal || _localOnly
          ? await _loadPreferLocal(preferred)
          : await _loader.load(
              detail.drama,
              detail.episodes[index],
              quality: _quality,
              online: _online,
              adBlock: _adBlock,
              route: preferred,
            );
      if (prepared == null) return;
      final plan = prepared;
      await _serialize(() async {
        if (!_current(ticket)) {
          await _release(plan.session);
          return;
        }
        if (plan.url.isEmpty) throw AppFailure('站源未返回播放地址，请重试');
        await _configure(player, plan);
        await _limitCache(player, !_engaged);
        _plan = plan;
        installed = true;
        _acceptErrors = true;
        // 手动切线路或自动换备线后，以实际生效的线路为准，后面的集数跟着走；
        // 正常切集时若这一集没有原来的线路，只记一笔日志、不改已选线路，
        // 下一集有这条线路时仍然回到它。
        if (switchRoute != null ||
            recovery == PlaybackRecoveryAction.alternative) {
          _route = plan.routeIndex;
        } else if (preferred > 0 && plan.routeIndex != preferred) {
          DiaryService.add(
            '[Play] 第 ${detail.episodes[index].number} 集没有线路 ${preferred + 1}，'
            '改用线路 ${plan.routeIndex + 1}/${plan.routeCount}',
          );
        }
        final autoPlay = _foreground && _playIntent;
        await player.open(
          Media(
            plan.url,
            httpHeaders: plan.headers,
            start: position > 0
                ? Duration(milliseconds: (position * 1000).round())
                : null,
          ),
          play: autoPlay,
        );
        if (!_current(ticket)) return;
        _openedIndex = index;
        _health.reset();
        await _setRate(_rate);
        if (!_current(ticket)) return;
        _loading = false;
        _notify();
        danmaku.setPlan(plan);
        _syncDanmaku();
        // autoPlay 为 false 只应发生在「用户主动暂停 / 退到后台」这类确实
        // 不想播的情况。若 open 那一瞬因前台标志抖动取到 false、但此刻播放
        // 意图仍成立，就补一次起播：否则新集地址与首帧都正常、唯独没在播，
        // 表现为换集后「画面动一下就卡住，要手动点一下才出播放按钮」。
        // 正常换集 autoPlay=true，整个 if 不执行，零副作用。
        if (!autoPlay &&
            _current(ticket) &&
            _foreground &&
            _playIntent &&
            _error == null) {
          unawaited(player.play().catchError((Object _) {}));
        }
      });
    } catch (error) {
      if (_current(ticket)) {
        if (prepared != null && identical(_plan, prepared)) {
          _acceptErrors = true;
          _queueRecovery('open_exception');
        } else {
          if (prepared != null) await _release(prepared.session);
          if (_current(ticket)) {
            DiaryService.add('[Play] 无法播放：${_diagnostic(error)}');
            _loading = false;
            _localFailure = error is AppFailure && error.code == 'local_media';
            _error = error is AppFailure ? error.message : '无法播放这一集，请重试或换一集。';
            _notify();
          }
        }
      } else if (prepared != null && !installed) {
        await _release(prepared.session);
      }
    } finally {
      if (warmed != null && !installed) await _release(warmed.session);
      if (retained != null) await _release(retained!.session);
    }
  }

  Future<void> _swap(int index, bool playWhenReady) async {
    final plan = _standbyPlan!;
    final next = _standby!;
    final previous = _player;
    final previousPlan = _plan;
    final volume = previous.state.volume;
    unawaited(saveProgress());
    _preloader.clear();
    final ticket = ++_generation;
    _seekSequence++;
    _pendingError = false;
    _errorTimer?.cancel();
    _health.reset();
    _recovery.reset();
    _playIntent = playWhenReady;
    _resumePosition = 0;
    _openedAt = 0;
    _index = index;
    _error = null;
    _localFailure = false;
    _loading = false;
    _standbyPlan = null;
    _standbyIndex = -1;
    _standbyOpen = false;
    _player = next;
    _standby = previous;
    _plan = plan;
    _bind();
    _openedIndex = index;
    _acceptErrors = true;
    unawaited(previous.stop().catchError((Object _) {}));
    if (previousPlan != null) unawaited(_release(previousPlan.session));
    danmaku.setPlan(plan);
    _notify();
    _syncDanmaku();
    // 备用播放器是用 play:false 预加载的（见 _open），换绑后必须先把「播放
    // 意图」落实成一次真实的 play()，再做缓存/音量/倍速这些配置。以前把
    // play() 排在三个 await 之后还带条件，任一 await 抖动或串行倍速队列卡住，
    // 新集就会停在「首帧已出、但没在播」：画面动一下就卡住，而 _playIntent
    // 仍是 true，连暂停按钮都不显示，只能靠手动点一下翻转状态才恢复。
    if (_current(ticket) && _foreground && _playIntent) {
      unawaited(next.play().catchError((Object _) {}));
    }
    try {
      await _limitCache(next, false);
      await next.setVolume(volume);
      await _setRate(_rate);
      // 配置过程中底层状态若回落到暂停，再补一次；正常播放时 state.playing
      // 已为 true，这里不会触发，不影响任何正常换集。
      if (_current(ticket) &&
          _foreground &&
          _playIntent &&
          !next.state.playing &&
          !next.state.completed) {
        unawaited(next.play().catchError((Object _) {}));
      }
    } catch (_) {
      if (_current(ticket)) _queueRecovery('open_exception');
    }
  }

  void _prime() {
    final standby = _standby;
    final next = _index + 1;
    if (_closed ||
        standby == null ||
        _standbyIndex >= 0 ||
        !_preloader.ready ||
        next >= detail.episodes.length) {
      return;
    }
    _standbyIndex = next;
    final plan = _preloader.take(
      detail.drama,
      detail.episodes[next],
      quality: _quality,
      online: _online,
      adBlock: _adBlock,
      route: _route,
    );
    if (plan == null) {
      _standbyIndex = -1;
      return;
    }
    _standbyPlan = plan;
    unawaited(_open(standby, plan));
  }

  Future<void> _open(Player standby, PlaybackPlan plan) async {
    try {
      await _configure(standby, plan);
      await _limitCache(standby, true);
      await standby.open(
        Media(plan.url, httpHeaders: plan.headers),
        play: false,
      );
    } catch (_) {
      if (identical(_standbyPlan, plan)) _dropStandby();
      return;
    }
    if (_closed || !identical(_standbyPlan, plan)) return;
    _standbyOpen = true;
    _standbyAt = DateTime.now();
    _notify();
  }

  void _dropStandby() {
    final plan = _standbyPlan;
    final opened = _standbyOpen || plan != null;
    _standbyPlan = null;
    _standbyIndex = -1;
    _standbyOpen = false;
    if (opened) unawaited(_standby?.stop().catchError((Object _) {}));
    if (plan != null) unawaited(_release(plan.session));
  }

  Future<void> _limitCache(Player target, bool limited) async {
    final platform = target.platform;
    if (platform is! NativePlayer) return;
    await platform.setProperty(
      'demuxer-max-bytes',
      '${(limited ? 4 : 32) * 1024 * 1024}',
    );
  }

  void start(int index, {double position = 0}) {
    if (!_usable) return;
    final wasEngaged = _engaged;
    _engaged = true;
    if (index != _index ||
        (position - _openedAt).abs() > .5 ||
        _error != null) {
      unawaited(play(index, position: position));
      return;
    }
    _playIntent = true;
    if (!wasEngaged) unawaited(_limitCache(player, false));
    if (ready) unawaited(player.play());
  }

  Future<void> retry() =>
      play(_index, position: currentPosition, playWhenReady: true);

  Future<void> switchOnline() {
    _online = true;
    return retry();
  }

  Future<bool> next() async {
    if (!hasNext) return false;
    await play(_index + 1);
    return true;
  }

  Future<bool> previous() async {
    if (!hasPrevious) return false;
    await play(_index - 1);
    return true;
  }

  void togglePlayback() {
    if (!_usable || !ready) return;
    endBoost();
    _playIntent = !player.state.playing;
    _health.reset();
    if (_playIntent && player.state.completed) {
      unawaited(play(_index));
      return;
    }
    if (_playIntent && _staleAudio) {
      _staleAudio = false;
      unawaited(player.seek(player.state.position).then((_) => player.play()));
    } else {
      unawaited(player.playOrPause());
    }
    if (!_playIntent) unawaited(saveProgress());
    _syncDanmaku();
  }

  Future<bool> seekTo(Duration target) async {
    if (!_usable || !ready) return false;
    final ticket = ++_seekSequence;
    final generation = _generation;
    danmaku.beginSeek();
    var succeeded = false;
    try {
      await player.seek(target);
      succeeded = true;
    } catch (_) {
    } finally {
      if (!_closed && ticket == _seekSequence && generation == _generation) {
        danmaku.endSeek(succeeded ? target : player.state.position);
      }
    }
    return succeeded;
  }

  Duration? seekBy(int seconds) {
    final duration = player.state.duration;
    if (!ready || duration <= Duration.zero) return null;
    final target = Duration(
      milliseconds: (player.state.position.inMilliseconds + seconds * 1000)
          .clamp(0, duration.inMilliseconds),
    );
    unawaited(seekTo(target));
    return target;
  }

  Future<void> _setRate(double value) {
    _rates = _rates
        .catchError((Object _) {})
        .then((_) => player.setRate(value));
    return _rates.catchError((Object _) {});
  }

  void beginBoost() {
    if (!ready || _boostTimer != null || _boosting) return;
    _boostTimer = Timer(const Duration(milliseconds: 350), () {
      _boostTimer = null;
      if (_closed ||
          !ready ||
          !player.state.playing ||
          player.state.completed) {
        return;
      }
      _boosting = true;
      unawaited(_setRate(_boostSpeed));
      _notify();
    });
  }

  void holdBoost() {
    if (!_usable || _boosting) return;
    _boostTimer?.cancel();
    _boostTimer = null;
    _boosting = true;
    unawaited(_setRate(_boostSpeed));
    _notify();
  }

  void setBoostLocked(bool locked) {
    if (_boostLocked == locked) return;
    if (locked) {
      holdBoost();
      _boostLocked = true;
      _notify();
    } else {
      _boostLocked = false;
      endBoost();
      _notify();
    }
  }

  bool endBoost() {
    if (_boostLocked) return false;
    final boosted = _boosting;
    _boostTimer?.cancel();
    _boostTimer = null;
    _boosting = false;
    if (boosted) {
      unawaited(_setRate(_speed));
      _notify();
    }
    return boosted;
  }

  void setVolume(double value) {
    final volume = value.clamp(0.0, 100.0);
    if (volume > 0) _unmutedVolume = volume;
    _applyVolume(volume);
  }

  void _applyVolume(double volume) {
    if (!player.state.playing) _staleAudio = true;
    unawaited(player.setVolume(volume));
  }

  double changeVolume(double delta) {
    final volume = (player.state.volume + delta).clamp(0.0, 100.0);
    setVolume(volume);
    return volume;
  }

  double toggleMute() {
    final current = player.state.volume;
    if (current > 0) _unmutedVolume = current;
    final target = current > 0 ? 0.0 : _unmutedVolume;
    _applyVolume(target);
    return target;
  }

  Future<void> _savePreferences(PlaybackPreferences preferences) async {
    if (!_usable) throw StateError('当前用户已变更');
    await store.setPlaybackPreferences(preferences);
    if (!_usable) throw StateError('当前用户已变更');
  }

  Future<void> setSpeed(double value) async {
    if (value == _speed) return;
    await _savePreferences(_preferences.copyWith(speed: value));
    _speed = value;
    _notify();
    if (!_boosting) await _setRate(value);
  }

  Future<void> setBoostSpeed(double value) async {
    if (value == _boostSpeed) return;
    await _savePreferences(_preferences.copyWith(boostSpeed: value));
    _boostSpeed = value;
    _notify();
    if (_boosting) await _setRate(value);
  }

  Future<void> setQuality(int value) async {
    if (value == _quality) return;
    await _savePreferences(_preferences.copyWith(quality: value));
    _quality = value;
    _preloader.clear();
    _notify();
    if (_plan?.local == true) return;
    await play(
      _index,
      position: currentPosition,
      playWhenReady: _error != null || player.state.playing,
    );
  }

  Future<void> toggleDanmaku() async {
    if (!danmakuSupported) return;
    final enabled = !_danmakuEnabled;
    await _savePreferences(_preferences.copyWith(danmaku: enabled));
    _danmakuEnabled = enabled;
    danmaku.setEnabled(enabled);
    _syncDanmaku();
    _notify();
  }

  Future<void> setAdBlock(bool value) async {
    if (value == _adBlock) return;
    await _savePreferences(_preferences.copyWith(adBlock: value));
    _adBlock = value;
    _preloader.clear();
    _notify();
    if (!ready || _plan?.local == true) return;
    await play(
      _index,
      position: currentPosition,
      playWhenReady: _error != null || player.state.playing,
    );
  }

  /// 手动切换到第 [route] 条线路（0 起）：保留当前进度，复用旧会话走
  /// selectRoute。只有存在多条可用线路时才生效。
  Future<void> setRoute(int route) async {
    final plan = _plan;
    if (plan == null || plan.local) return;
    if (route < 0 || route >= plan.routeCount) return;
    if (route == plan.routeIndex) return;
    DiaryService.add(
      '[Play] 手动切换线路 ${route + 1}/${plan.routeCount} session=${plan.session}',
    );
    await play(
      _index,
      position: currentPosition,
      playWhenReady: _error != null || player.state.playing,
      switchRoute: route,
    );
  }

  void setForeground(bool visible) {
    if (_closed || _foreground == visible) return;
    _foreground = visible;
    _health.reset();
    _sync();
    if (!visible) {
      endBoost();
      _playIntent = false;
      unawaited(player.pause());
      unawaited(saveProgress());
    } else if (_pendingError) {
      _queueRecovery('foreground_pending_error');
    }
  }

  Future<void> saveProgress() async {
    if (!_engaged || _openedIndex < 0 || store.profileEpoch != _epoch) return;
    final position = player.state.position.inMilliseconds / 1000;
    final duration = player.state.duration.inMilliseconds / 1000;
    if (position < .1) return;
    final key = '${detail.drama.id}:$_openedIndex:$position:$duration';
    if (_savedProgressKey == key && _saveWarning == null) return;
    final entry = WatchEntry(
      drama: repository.catalogUpdates.current(detail.drama),
      episode: detail.episodes[_openedIndex].number,
      position: position,
      duration: duration,
      updatedAt: DateTime.now(),
    );
    try {
      await store.saveWatch(entry);
      _savedProgressKey = key;
      if (_saveWarning != null) {
        _saveWarning = null;
        _notify();
      }
    } catch (_) {
      if (_saveWarning == null) {
        _saveWarning = '观看进度尚未保存，请检查存储空间后重试。';
        _notify();
      }
    }
  }

  @override
  void dispose() {
    unawaited(saveProgress());
    _closed = true;
    _generation++;
    store.removeListener(_accessChanged);
    _saveTimer?.cancel();
    _healthTimer?.cancel();
    _errorTimer?.cancel();
    _boostTimer?.cancel();
    _stallTimer?.cancel();
    for (final subscription in _subscriptions) {
      unawaited(subscription.cancel());
    }
    danmaku.dispose();
    _preloader.dispose();
    final session = _plan?.session ?? '';
    unawaited(_release(session));
    final standbyPlan = _standbyPlan;
    if (standbyPlan != null) unawaited(_release(standbyPlan.session));
    final standby = _standby;
    unawaited(_loader.close().catchError((Object _) {}));
    unawaited(
      _operations.catchError((Object _) {}).then((_) async {
        await _rates.catchError((Object _) {});
        await player.dispose();
        await standby?.dispose();
      }),
    );
    super.dispose();
  }
}
