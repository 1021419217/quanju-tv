import 'package:flutter/material.dart';

import '../../local_store.dart';
import '../../models.dart';
import '../app_scope.dart';
import '../shell/app_shell.dart';
import '../widgets/drama_grid.dart';
import '../widgets/page_header.dart';
import 'detail_page.dart';

/// 作者主页：某个发布者上传的其它作品。
///
/// 目前只有 91porn 提供作者主页（站方 `/uvideos.php?UID=...`），
/// 数据走原生 `authorCatalog`，返回结构与普通目录一致，直接复用同一套网格。
class AuthorPage extends StatefulWidget {
  const AuthorPage({
    super.key,
    required this.store,
    required this.source,
    required this.authorId,
    required this.authorName,
  });

  final LocalStore store;
  final String source;
  final String authorId;
  final String authorName;

  @override
  State<AuthorPage> createState() => _AuthorPageState();
}

class _AuthorPageState extends State<AuthorPage> {
  final _items = <Drama>[];
  final _scroll = ScrollController();
  var _page = 0;
  var _hasMore = true;
  var _loading = false;
  var _started = false;
  String _error = '';

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_maybeLoadMore);
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_started) return;
    _started = true;
    _load();
  }

  @override
  void dispose() {
    _scroll.removeListener(_maybeLoadMore);
    _scroll.dispose();
    super.dispose();
  }

  void _maybeLoadMore() {
    if (!_scroll.hasClients) return;
    if (_scroll.position.pixels < _scroll.position.maxScrollExtent - 320) return;
    _load();
  }

  Future<void> _load() async {
    if (_loading || !_hasMore) return;
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final result = await AppScope.of(context).repository.authorCatalog(
        widget.source,
        widget.authorId,
        page: _page + 1,
      );
      if (!mounted) return;
      final known = {for (final item in _items) item.id};
      setState(() {
        _page += 1;
        _items.addAll(result.items.where((item) => known.add(item.id)));
        _hasMore = result.hasMore && result.items.isNotEmpty;
        _loading = false;
      });
    } catch (failure) {
      if (!mounted) return;
      setState(() {
        _error = failure is StateError ? failure.message : '$failure';
        _loading = false;
      });
    }
  }

  void _open(Drama drama) => openPage<void>(
    context,
    (_) => DetailPage(store: widget.store, drama: drama),
  );

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(widget.authorName.isEmpty ? '作者主页' : widget.authorName)),
      body: LayoutBuilder(
        builder: (context, constraints) {
          final inset = constraints.maxWidth >= 720 ? 24.0 : 16.0;
          return CustomScrollView(
            controller: _scroll,
            slivers: [
              SliverPadding(
                padding: EdgeInsets.fromLTRB(inset, 8, inset, 0),
                sliver: SliverToBoxAdapter(
                  child: PageHeader(
                    title: widget.authorName.isEmpty ? '作者主页' : widget.authorName,
                    count: _items.isEmpty ? null : '${_items.length}',
                  ),
                ),
              ),
              if (_items.isEmpty && _loading)
                const SliverFillRemaining(
                  hasScrollBody: false,
                  child: Center(child: CircularProgressIndicator()),
                )
              else if (_items.isEmpty)
                SliverFillRemaining(
                  hasScrollBody: false,
                  child: Center(
                    child: Padding(
                      padding: EdgeInsets.all(inset),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Text(
                            _error.isEmpty ? '这位作者暂时没有其它作品' : _error,
                            textAlign: TextAlign.center,
                            style: theme.textTheme.bodyMedium,
                          ),
                          if (_error.isNotEmpty) ...[
                            const SizedBox(height: 12),
                            FilledButton.tonal(
                              onPressed: () {
                                _hasMore = true;
                                _load();
                              },
                              child: const Text('重试'),
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
                )
              else
                SliverPadding(
                  padding: EdgeInsets.fromLTRB(inset, 16, inset, 0),
                  sliver: DramaSliverGrid(
                    width: constraints.maxWidth - inset * 2,
                    items: _items,
                    onOpen: _open,
                  ),
                ),
              SliverPadding(
                padding: EdgeInsets.fromLTRB(inset, 16, inset, 32),
                sliver: SliverToBoxAdapter(
                  child: Center(
                    child: _loading && _items.isNotEmpty
                        ? const SizedBox(
                            width: 22,
                            height: 22,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : Text(
                            _hasMore ? '继续上滑加载更多' : '没有更多了',
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          ),
                  ),
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}
