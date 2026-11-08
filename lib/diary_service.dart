import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';

class DiaryService {
  static final List<String> _entries = <String>[];
  static const int maxEntries = 500;
  static String? nativeLogPath;

  /// 原生核心的标准错误落盘位置（见 bridge 的 DuanjuInstallStderrLog）。
  /// 核心内部 panic 只会对外回一句「本地核心处理失败，请重试」，真正有用的
  /// 堆栈只写在这里，所以要和普通日志一起展示。
  static String? nativeStderrLogPath;
  static String _nativeText = '';
  static String _crashText = '';
  static int _clears = 0;
  static final ValueNotifier<int> revision = ValueNotifier<int>(0);

  static void add(String message) {
    final entry = '[${_clock(DateTime.now())}] $message';
    _entries.add(entry);
    if (_entries.length > maxEntries) {
      _entries.removeRange(0, _entries.length - maxEntries);
    }
    revision.value++;
    debugPrint('[Diary] $message');
  }

  static List<String> get entries => List<String>.unmodifiable([
    ..._entries,
    if (_nativeText.isNotEmpty) ..._nativeText.split('\n'),
    if (_crashText.isNotEmpty) ..._crashText.split('\n'),
  ]);

  /// 最近一条原生流诊断里的失败原因。
  ///
  /// 播放失败时界面只能给一句「自动恢复未成功」，真正的原因（证书校验失败 /
  /// 连不上 CDN / 403 / 超时）只在原生日志里。把最后一条拿出来贴在错误文案里，
  /// 用户截图就能定位，不用再让他翻日记。
  static String? lastStreamIssue() {
    if (_nativeText.isEmpty) return null;
    final lines = _nativeText.split('\n');
    final start = lines.length > 80 ? lines.length - 80 : 0;
    for (var i = lines.length - 1; i >= start; i--) {
      final line = lines[i];
      if (!line.contains('[Stream]')) continue;
      // 续传相关事件单独看：relay_resume 是「断了但接上了」的正常过程，
      // 不能当失败原因；只有放弃和换线失败才是真原因。
      if (line.contains('relay_give_up')) return '连续断流，续传未恢复';
      if (line.contains('relay_resume_failed')) return '断流后无法续传';
      // 同一句「传输中断」可能来自主站、CDN 或分片，带上域名才能定位是谁在断。
      final target = RegExp(r'path=([^\s/]+)').firstMatch(line)?.group(1);
      final where = (target == null || target.isEmpty) ? '' : '（$target）';
      final error = RegExp(r'error=([a-z_]+)').firstMatch(line)?.group(1);
      if (error != null && error != 'none') {
        return '${_streamIssueText(error)}$where';
      }
      final status = RegExp(r'HTTP (\d{3})').firstMatch(line)?.group(1);
      if (status != null) return '站方 CDN 返回 HTTP $status$where';
      if (line.contains('asset_missing')) return '播放会话已过期';
    }
    return null;
  }

  static String _streamIssueText(String code) {
    switch (code) {
      case 'tls':
        return '证书校验失败';
      case 'dns':
        return '域名解析失败';
      case 'connect':
        return '连不上站方 CDN';
      case 'timeout':
      case 'deadline':
        return '连接超时';
      case 'unexpected_eof':
        return '连接被中断';
      case 'canceled':
        return '请求被取消';
      default:
        return '传输中断';
    }
  }

  static Future<void> clear() async {
    _entries.clear();
    _nativeText = '';
    _crashText = '';
    _clears++;
    revision.value++;
    final path = nativeLogPath;
    if (path == null) return;
    for (final name in [path, '$path.1', '$path.2']) {
      try {
        await File(name).delete();
      } on FileSystemException {
        continue;
      }
    }
    final crash = nativeStderrLogPath;
    if (crash != null) {
      try {
        await File(crash).delete();
      } on FileSystemException {
        // 忽略：文件不存在或核心正持有句柄。
      }
    }
  }

  static String get fullText =>
      [_entries.join('\n'), _nativeText, _crashText].join('\n');

  static Future<void> refreshNativeLogs() async {
    final path = nativeLogPath;
    final clears = _clears;
    final lines = <String>[];
    if (path != null) {
      for (final name in ['$path.1', path]) {
        RandomAccessFile? file;
        try {
          file = await File(name).open();
          final size = await file.length();
          final start = size > 128 * 1024 ? size - 128 * 1024 : 0;
          await file.setPosition(start);
          final text = utf8.decode(
            await file.read(size - start),
            allowMalformed: true,
          );
          for (final line in text.split('\n').skip(start > 0 ? 1 : 0)) {
            try {
              final event = jsonDecode(line) as Map<String, dynamic>;
              final name = event['event'] as String? ?? '';
              if (name.startsWith('stream.')) {
                lines.add(_nativeLine(event, name.substring('stream.'.length)));
              }
            } catch (_) {}
          }
        } on FileSystemException {
          continue;
        } finally {
          await file?.close();
        }
      }
    }
    final crash = await _readStderrTail();
    if (clears != _clears) return;
    _nativeText = lines
        .skip(lines.length > 500 ? lines.length - 500 : 0)
        .join('\n');
    _crashText = crash;
  }

  /// 读取原生核心 stderr 日志尾部。这里不做解析：Go 的 panic 堆栈是纯文本，
  /// 原样保留（含 goroutine 编号与调用链）才有定位价值。
  static Future<String> _readStderrTail() async {
    final path = nativeStderrLogPath;
    if (path == null) return '';
    RandomAccessFile? file;
    final lines = <String>[];
    try {
      file = await File(path).open();
      final size = await file.length();
      if (size == 0) return '';
      final start = size > 128 * 1024 ? size - 128 * 1024 : 0;
      await file.setPosition(start);
      final text = utf8.decode(
        await file.read(size - start),
        allowMalformed: true,
      );
      for (final line in text.split('\n').skip(start > 0 ? 1 : 0)) {
        if (line.trim().isEmpty) continue;
        lines.add(line);
      }
    } on FileSystemException {
      return '';
    } finally {
      await file?.close();
    }
    if (lines.isEmpty) return '';
    final kept = lines.skip(lines.length > 400 ? lines.length - 400 : 0);
    return ['[原生核心日志]', ...kept].join('\n');
  }

  static String _nativeLine(Map<String, dynamic> event, String name) {
    final time = DateTime.parse(event['time'] as String).toLocal();
    final status = event['httpStatus'];
    return [
      '[${_clock(time)}] [Stream] $name',
      event['host'],
      if (status != null) 'HTTP $status',
      event['responseType'],
      event['message'],
    ].whereType<String>().where((part) => part.isNotEmpty).join(' ');
  }

  static String _clock(DateTime time) =>
      '${time.hour.toString().padLeft(2, '0')}:${time.minute.toString().padLeft(2, '0')}:${time.second.toString().padLeft(2, '0')}.${time.millisecond ~/ 100}';
}
