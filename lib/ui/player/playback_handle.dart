import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import '../../core_bridge.dart';
import '../../local_store.dart';
import '../../models.dart';
import '../../playback_session.dart';

Player _player() => Player(
  configuration: const PlayerConfiguration(
    bufferSize: 32 * 1024 * 1024,
    logLevel: MPVLogLevel.error,
  ),
);

class PlaybackHandle {
  PlaybackHandle({
    required AppRepository repository,
    required LocalStore store,
    required DramaDetail detail,
    required int index,
    double position = 0,
    bool autoplay = true,
    bool preferLocal = true,
    bool localOnly = false,
  }) {
    final main = _player();
    final standby = _player();
    _videos[main] = VideoController(main);
    _videos[standby] = VideoController(standby);
    session = PlaybackSession(
      repository: repository,
      store: store,
      detail: detail,
      player: main,
      standby: standby,
      index: index,
      position: position,
      autoplay: autoplay,
      preferLocal: preferLocal,
      localOnly: localOnly,
    );
  }

  late final PlaybackSession session;
  final _videos = <Player, VideoController>{};

  VideoController video(Player player) => _videos[player]!;

  void dispose() => session.dispose();
}
