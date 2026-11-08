import 'package:flutter/foundation.dart';

import 'models.dart';

typedef PageLoader = Future<CatalogPage> Function(int page, bool force);

class DramaFeed extends ChangeNotifier {
  DramaFeed({required this.load, this.cached, this.cumulative = false});
  final PageLoader load;
  final Future<CatalogPage> Function()? cached;
  final bool cumulative;

  List<Drama> _items = const [];
  bool _loading = false;
  bool _loadingMore = false;
  bool _hasMore = false;
  String _error = '';
  int _next = 1;
  int _generation = 0;
  bool _disposed = false;

  List<Drama> get items => _items;
  bool get loading => _loading;
  bool get loadingMore => _loadingMore;
  bool get hasMore => _hasMore;
  String get error => _error;
  bool get canLoadMore =>
      _hasMore && !_loading && !_loadingMore && _error.isEmpty;

  Future<void> reload({bool force = false}) async {
    final generation = ++_generation;
    _loading = true;
    _loadingMore = false;
    _error = '';
    _notify();
    if (!force && cached != null) {
      try {
        final page = await cached!();
        if (generation != _generation) return;
        if (page.items.isNotEmpty) {
          _accept(page, more: false, requested: page.page);
          if (page.fresh && page.warning.isEmpty) {
            _loading = false;
            _notify();
            return;
          }
          _notify();
        }
      } catch (_) {}
    }
    await _fetch(generation, page: 1, force: force, more: false);
  }

  Future<void> loadMore() async {
    if (_loading || _loadingMore || !_hasMore) return;
    final generation = ++_generation;
    _loadingMore = true;
    _error = '';
    _notify();
    await _fetch(generation, page: _next, force: false, more: true);
  }

  void update(Drama drama) {
    final index = _items.indexWhere((item) => item.id == drama.id);
    if (index < 0) return;
    final merged = _items[index].merge(drama);
    // 详情页加载完会把补全后的条目推回来。多数时候只补了一两个字段，
    // 这时候重建整格会让封面重新加载、整屏闪一下；没有实质变化就不通知。
    if (_sameDrama(merged, _items[index])) return;
    _items = [
      for (var i = 0; i < _items.length; i++)
        i == index ? merged : _items[i],
    ];
    _notify();
  }

  bool _sameDrama(Drama a, Drama b) =>
      a.title == b.title &&
      a.cover == b.cover &&
      a.description == b.description &&
      a.episodes == b.episodes &&
      a.category == b.category &&
      a.vipStatus == b.vipStatus &&
      a.heat == b.heat &&
      a.views == b.views &&
      a.onlineDate == b.onlineDate &&
      a.releaseStatus == b.releaseStatus &&
      listEquals(a.tags, b.tags);

  Future<void> _fetch(
    int generation, {
    required int page,
    required bool force,
    required bool more,
  }) async {
    try {
      final result = await load(page, force);
      if (generation != _generation) return;
      _accept(result, more: more, requested: page);
    } catch (error) {
      if (generation != _generation) return;
      _error = error.toString();
    }
    _loading = _loadingMore = false;
    _notify();
  }

  void _accept(CatalogPage page, {required bool more, required int requested}) {
    if (cumulative || !more) {
      _items = page.items;
    } else {
      final known = {for (final item in _items) item.id};
      _items = [
        ..._items,
        for (final item in page.items)
          if (known.add(item.id)) item,
      ];
    }
    final failed = page.warning.isNotEmpty;
    _next = failed ? requested : page.page + 1;
    _hasMore = page.hasMore || failed;
    _error = page.warning;
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    super.dispose();
  }
}
