import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../follow_list.dart';
import '../../local_store.dart';
import '../../models.dart';
import '../app_scope.dart';
import '../detail/detail_page.dart';
import '../settings/settings_rows.dart';
import '../shell/app_shell.dart';
import '../widgets/drama_grid.dart';
import '../widgets/drama_tile.dart';
import '../widgets/focus_ring.dart';
import '../widgets/frosted_header.dart';
import '../widgets/page_header.dart';
import '../widgets/search_field.dart';
import '../widgets/status_view.dart';
import '../widgets/swipe_category_layer.dart';

typedef _View = ({
  List<Drama> follows,
  List<SourceSite> sources,
  String source,
  List<Drama> shown,
  Set<String>? selection,
});

class FollowPage extends StatefulWidget {
  const FollowPage({
    super.key,
    required this.store,
    required this.entryFocus,
    required this.onExitUp,
    this.onOpenDownloads,
  });
  final LocalStore store;
  final FocusNode entryFocus;
  final VoidCallback onExitUp;

  /// 「我的下载」入口：放在追剧页顶栏，首页保持干净。
  final VoidCallback? onOpenDownloads;

  @override
  State<FollowPage> createState() => _FollowPageState();
}

class _FollowPageState extends State<FollowPage> {
  final _query = TextEditingController();
  final _editButton = FocusNode();
  var _source = '';
  Set<String>? _selection;

  @override
  void dispose() {
    _query.dispose();
    _editButton.dispose();
    super.dispose();
  }

  void _keepFocus(FocusNode target) =>
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !AppScope.of(context).television) return;
        final focused = FocusManager.instance.primaryFocus;
        if (focused != null && focused is! FocusScopeNode) return;
        if (target.context?.mounted ?? false) {
          revealFocus(target);
        } else {
          widget.onExitUp();
        }
      });

  void _startEditing() {
    setState(() => _selection = {});
    _keepFocus(widget.entryFocus);
  }

  void _finishEditing() {
    setState(() => _selection = null);
    _keepFocus(_editButton);
  }

  void _toggle(String id) => setState(() {
    final selection = {...?_selection};
    if (!selection.remove(id)) selection.add(id);
    _selection = selection;
  });

  void _open(Drama drama) {
    if (widget.store.following(drama.id)?.hasUpdates ?? false) {
      saveSetting(context, () => widget.store.markUpdatesRead(drama.id));
    }
    openPage<void>(
      context,
      (_) => DetailPage(store: widget.store, drama: drama),
    );
  }

  Future<void> _unfollow(Set<String> ids) async {
    if (ids.isEmpty) return;
    final colors = Theme.of(context).colorScheme;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('取消追剧 ${ids.length} 部？'),
        content: const Text('观看记录会保留。'),
        actions: [
          FocusRing(
            radius: 20,
            child: TextButton(
              autofocus: true,
              onPressed: () => Navigator.pop(context, false),
              child: const Text('取消'),
            ),
          ),
          FocusRing(
            radius: 20,
            child: FilledButton(
              style: FilledButton.styleFrom(
                backgroundColor: colors.error,
                foregroundColor: colors.onError,
              ),
              onPressed: () => Navigator.pop(context, true),
              child: const Text('取消追剧'),
            ),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    await saveSetting(context, () => widget.store.removeFavorites(ids));
    if (!mounted) return;
    setState(() => _selection = null);
    _keepFocus(widget.entryFocus);
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: Listenable.merge([widget.store, _query]),
    builder: (context, _) {
      final follows = widget.store.favorites;
      final sources = followedSources(follows);
      final source = sources.any((site) => site.id == _source) ? _source : '';
      final shown = [
        for (final drama in follows)
          if ((source.isEmpty || drama.source == source) &&
              matchesFollowTitle(drama, _query.text))
            drama,
      ];
      final view = (
        follows: follows,
        sources: sources,
        source: source,
        shown: shown,
        selection: _selection?.intersection({
          for (final drama in shown) drama.id,
        }),
      );
      return PopScope(
        canPop: _selection == null,
        onPopInvokedWithResult: (didPop, _) {
          if (!didPop) _finishEditing();
        },
        child: switch (AppScope.formFactorOf(context)) {
          FormFactor.phone => _phone(context, view),
          FormFactor.pad || FormFactor.window => _wide(context, view),
          FormFactor.television => _television(context, view),
        },
      );
    },
  );

  Set<String> _ids(_View view) => {for (final drama in view.shown) drama.id};

  Widget _search({FocusNode? focusNode}) => SearchField(
    hint: '搜索追剧',
    controller: _query,
    focusNode: focusNode,
    onSubmitted: (_) {},
    letterKeyboard: widget.store.televisionKeyboard,
    matches: (query) async => {
      for (final drama in widget.store.favorites)
        if (matchesFollowTitle(drama, query)) drama.title,
    }.toList(),
  );

  Widget _textButton(
    String label,
    VoidCallback? onPressed, {
    IconData? icon,
    FocusNode? focusNode,
  }) {
    // 手机端顶栏文字按钮换成与首页同款的低对比磨砂胶囊；
    // 电视端保留实心 TextButton，遥控器焦点更清楚。
    if (AppScope.formFactorOf(context) != FormFactor.television) {
      return GlassAction(
        label: label,
        icon: icon,
        onPressed: onPressed,
        focusNode: focusNode,
      );
    }
    return FocusRing(
      radius: 20,
      child: icon == null
          ? TextButton(
              focusNode: focusNode,
              onPressed: onPressed,
              child: Text(label),
            )
          : TextButton.icon(
              focusNode: focusNode,
              onPressed: onPressed,
              icon: Icon(icon, size: 18),
              label: Text(label),
            ),
    );
  }

  /// 「我的下载」与「编辑」各自是一枚独立的玻璃胶囊（图标 + 文字），
  /// 不再让圆形图标按钮紧贴着「编辑」两个字。电视端 _textButton 会自动
  /// 换成实心 TextButton.icon，保持遥控器焦点清晰。
  Widget? _downloadButton() {
    final open = widget.onOpenDownloads;
    if (open == null) return null;
    return _textButton('下载', open, icon: Icons.download_rounded);
  }

  Widget _editButtonFor(_View view, {bool icon = true}) => _textButton(
    '编辑',
    view.shown.isEmpty ? null : _startEditing,
    icon: icon ? Icons.check_box_outlined : null,
    focusNode: _editButton,
  );

  Widget _selectAll(_View view) {
    final all = _ids(view);
    final everything = view.selection!.length == all.length;
    return _textButton(
      everything ? '取消全选' : '全选',
      () => setState(() => _selection = everything ? {} : all),
    );
  }

  Widget _unfollowButton(_View view) {
    final colors = Theme.of(context).colorScheme;
    final selection = view.selection!;
    return FocusRing(
      radius: 20,
      child: FilledButton.icon(
        style: FilledButton.styleFrom(
          backgroundColor: colors.error,
          foregroundColor: colors.onError,
        ),
        onPressed: selection.isEmpty ? null : () => _unfollow(selection),
        icon: const Icon(Icons.bookmark_remove_outlined, size: 18),
        label: const Text('取消追剧'),
      ),
    );
  }

  List<Widget> _editActions(_View view) => [
    _selectAll(view),
    _unfollowButton(view),
    _textButton('完成', _finishEditing),
  ];

  Widget _title(String text, double size) => Semantics(
    header: true,
    child: Text(
      text,
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      style: TextStyle(fontSize: size, fontWeight: FontWeight.w800),
    ),
  );

  Widget _sourceChip(String id, String label, _View view) =>
      AppScope.formFactorOf(context) == FormFactor.television
      ? FocusRing(
          radius: 8,
          child: ChoiceChip(
            key: ValueKey('follow-source-$id'),
            label: Text(label),
            showCheckmark: false,
            selected: view.source == id,
            onSelected: (_) => setState(() => _source = id),
          ),
        )
      : GlassChip(
          key: ValueKey('follow-source-$id'),
          label: label,
          selected: view.source == id,
          onTap: () => setState(() => _source = id),
          height: 34,
        );

  Widget? _sources(_View view, {required double inset, bool scroll = false}) {
    if (view.sources.length < 2) return null;
    final chips = [
      _sourceChip('', '全部', view),
      for (final site in view.sources) _sourceChip(site.id, site.name, view),
    ];
    if (scroll) {
      return SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        padding: EdgeInsets.fromLTRB(inset, 4, inset, 8),
        child: Row(spacing: 8, children: chips),
      );
    }
    return Padding(
      padding: EdgeInsets.fromLTRB(inset, 4, inset, 8),
      child: Wrap(spacing: 8, runSpacing: 8, children: chips),
    );
  }

  Widget _phone(BuildContext context, _View view) {
    final selection = view.selection;
    final header = <Widget>[
      if (selection == null)
        PageHeader(
          title: '追剧',
          count: '${view.shown.length}',
          actions: [
            ?_downloadButton(),
            if (widget.onOpenDownloads != null) const SizedBox(width: 10),
            _editButtonFor(view),
          ],
        )
      else
        PageHeader(
          title: '已选择 ${selection.length} 部',
          actions: [_selectAll(view), _textButton('完成', _finishEditing)],
        ),
      Padding(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
        child: _search(),
      ),
      ?_sources(view, inset: 16, scroll: true),
    ];
    // 与首页一致：顶栏浮在内容之上做磨砂，内容延伸到屏顶并在首部留白，
    // 滚动时剧集卡片从玻璃背后透出。
    final headerHeight =
        62.0 + 48.0 + (view.sources.length > 1 ? 46.0 : 0.0) + 8.0;
    final topInset = MediaQuery.paddingOf(context).top + headerHeight;
    return Scaffold(
      body: Stack(
        children: [
          Positioned.fill(
            child: _content(
              context,
              view,
              inset: 16,
              top: topInset,
              header: const [],
            ),
          ),
          Positioned(
            top: 0,
            left: 0,
            right: 0,
            child: FrostedHeader(
              child: SafeArea(
                bottom: false,
                child: Column(mainAxisSize: MainAxisSize.min, children: header),
              ),
            ),
          ),
        ],
      ),
      bottomNavigationBar: selection == null
          ? null
          : Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
              child: SizedBox(height: 48, child: _unfollowButton(view)),
            ),
    );
  }

  Widget _wide(BuildContext context, _View view) => Scaffold(
    body: SafeArea(
      bottom: false,
      child: _content(
        context,
        view,
        inset: 24,
        header: [
          LayoutBuilder(
            builder: (context, constraints) => Padding(
              padding: EdgeInsets.fromLTRB(
                view.selection == null ? 24 : 12,
                8,
                24,
                8,
              ),
              child: SizedBox(
                height: 48,
                child: Row(
                  spacing: 8,
                  children: [
                    if (view.selection == null) ...[
                      Expanded(child: _title('追剧', 28)),
                      SizedBox(
                        width: (constraints.maxWidth - 400).clamp(160.0, 280.0),
                        child: _search(),
                      ),
                      ?_downloadButton(),
                      _editButtonFor(view),
                    ] else ...[
                      IconButton(
                        tooltip: '退出编辑',
                        onPressed: _finishEditing,
                        icon: const Icon(Icons.close_rounded),
                      ),
                      Expanded(
                        child: _title('已选择 ${view.selection!.length} 部', 28),
                      ),
                      ..._editActions(view),
                    ],
                  ],
                ),
              ),
            ),
          ),
          ?_sources(view, inset: 24),
        ],
      ),
    ),
  );

  Widget _television(BuildContext context, _View view) => Focus(
    canRequestFocus: false,
    skipTraversal: true,
    onKeyEvent: (_, event) {
      if (event is KeyUpEvent ||
          event.logicalKey != LogicalKeyboardKey.arrowUp) {
        return KeyEventResult.ignored;
      }
      final moved = FocusManager.instance.primaryFocus?.focusInDirection(
        TraversalDirection.up,
      );
      if (moved != true) widget.onExitUp();
      return KeyEventResult.handled;
    },
    child: Scaffold(
      body: SafeArea(
        top: false,
        child: _content(
          context,
          view,
          inset: televisionInset,
          top: 14,
          header: [
            if (view.follows.isNotEmpty || view.selection != null)
              Padding(
                padding: const EdgeInsets.fromLTRB(
                  televisionInset,
                  0,
                  televisionInset,
                  8,
                ),
                child: SizedBox(
                  height: 48,
                  child: Row(
                    spacing: 12,
                    children: view.selection == null
                        ? [
                            SizedBox(
                              width: 260,
                              child: _search(
                                focusNode: view.shown.isEmpty
                                    ? widget.entryFocus
                                    : null,
                              ),
                            ),
                            const Spacer(),
                            ?_downloadButton(),
                            _editButtonFor(view),
                          ]
                        : [
                            Expanded(
                              child: _title(
                                '已选择 ${view.selection!.length} 部',
                                24,
                              ),
                            ),
                            ..._editActions(view),
                          ],
                  ),
                ),
              ),
            ?_sources(view, inset: televisionInset),
          ],
        ),
      ),
    ),
  );

  (int, double, double) _grid(BuildContext context, double width) {
    switch (AppScope.formFactorOf(context)) {
      case FormFactor.phone:
        return (3, 10, 20);
      case FormFactor.television:
        return (6, 20, 24);
      case FormFactor.pad || FormFactor.window:
        final desktop = AppScope.formFactorOf(context) == FormFactor.window;
        final minimum = desktop ? 140.0 : 120.0;
        final gap = desktop ? 18.0 : 16.0;
        final columns = ((width + gap) / (minimum + gap)).floor().clamp(1, 99);
        return (columns, gap, desktop ? 22 : 20);
    }
  }

  Widget _content(
    BuildContext context,
    _View view, {
    required double inset,
    double top = 0,
    required List<Widget> header,
  }) => LayoutBuilder(
    builder: (context, constraints) {
      final width = constraints.maxWidth - inset * 2;
      final (columns, gap, runGap) = _grid(context, width);
      final tile = (width - gap * (columns - 1)) / columns;
      final updated = [
        for (final drama in view.shown)
          if (widget.store.following(drama.id)?.hasUpdates ?? false) drama,
      ];
      final rest = [
        for (final drama in view.shown)
          if (!updated.contains(drama)) drama,
      ];
      final sections = <(String?, List<Drama>)>[
        if (updated.isNotEmpty) ('有更新', updated),
        if (rest.isNotEmpty) (updated.isEmpty ? null : '其他追剧', rest),
      ];
      final first = view.shown.isEmpty
          ? null
          : (updated.firstOrNull ?? rest.first);
      // 与首页一致：左右快滑在「全部 / 各站源」之间切换分类。
      // 电视端组件会自动跳过手势，遥控器焦点行为不变。
      final catIds = ['', for (final site in view.sources) site.id];
      final catLabels = ['全部', for (final site in view.sources) site.name];
      var catIndex = catIds.indexOf(view.source);
      if (catIndex < 0) catIndex = 0;
      return SwipeCategoryLayer(
        itemCount: view.sources.length < 2 ? 1 : catIds.length,
        index: catIndex,
        labels: catLabels,
        onIndexChanged: (next) => setState(() => _source = catIds[next]),
        child: CustomScrollView(
        slivers: [
          SliverPadding(
            padding: EdgeInsets.only(top: top),
            sliver: SliverList.list(children: header),
          ),
          if (view.shown.isEmpty)
            SliverFillRemaining(
              hasScrollBody: false,
              child: StatusView(
                title: view.follows.isEmpty ? '还没有追剧' : '没有匹配的追剧',
                icon: view.follows.isEmpty
                    ? Icons.bookmark_border_rounded
                    : Icons.search_off_rounded,
              ),
            )
          else
            for (final (label, dramas) in sections) ...[
              if (label != null)
                SliverPadding(
                  padding: EdgeInsets.fromLTRB(inset + 4, 12, inset, 10),
                  sliver: SliverToBoxAdapter(
                    child: Semantics(
                      header: true,
                      child: Text.rich(
                        TextSpan(
                          text: label,
                          children: [
                            TextSpan(
                              text: '  ${dramas.length}',
                              style: TextStyle(
                                fontSize: 14,
                                fontWeight: FontWeight.w500,
                                color: Theme.of(
                                  context,
                                ).colorScheme.onSurfaceVariant,
                              ),
                            ),
                          ],
                        ),
                        style: const TextStyle(
                          fontSize: 18,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                  ),
                ),
              SliverPadding(
                padding: EdgeInsets.fromLTRB(
                  inset,
                  label == null ? 12 : 0,
                  inset,
                  0,
                ),
                sliver: SliverGrid.builder(
                  gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                    crossAxisCount: columns,
                    crossAxisSpacing: gap,
                    mainAxisSpacing: runGap,
                    mainAxisExtent: ProgressPoster.extentFor(context, tile),
                  ),
                  itemCount: dramas.length,
                  itemBuilder: (context, index) {
                    final drama = dramas[index];
                    return Focus(
                      key: ValueKey(drama.id),
                      canRequestFocus: false,
                      skipTraversal: true,
                      onKeyEvent: (_, event) =>
                          wrapGridRow(index, columns, dramas.length, event),
                      child: _poster(
                        context,
                        drama,
                        tile,
                        view.selection,
                        focusNode: drama == first ? widget.entryFocus : null,
                      ),
                    );
                  },
                ),
              ),
            ],
          SliverPadding(
            padding: EdgeInsets.only(
              bottom: AppScope.of(context).television
                  ? 27
                  : (AppScope.formFactorOf(context) == FormFactor.phone
                        ? 100
                        : 32),
            ),
          ),
        ],
        ),
      );
    },
  );

  Widget _poster(
    BuildContext context,
    Drama drama,
    double width,
    Set<String>? selection, {
    FocusNode? focusNode,
  }) {
    final watch = widget.store.watched(drama.id);
    final state = widget.store.following(drama.id);
    final total = drama.episodes;
    final String caption;
    if (watch == null) {
      caption = total > 0 ? '未观看 · 共 $total 集' : '未观看';
    } else if (total > 0 && watch.episode >= total && watch.finished) {
      caption = '已看完';
    } else {
      final long = '看到第 ${watch.episode} 集 / 共 $total 集';
      caption = total > 0 && _fits(context, long, width)
          ? long
          : '看到第 ${watch.episode} 集';
    }
    return ProgressPoster(
      drama: drama,
      width: width,
      focusNode: focusNode,
      progress: watch != null && total > 0 ? watch.episode / total : 0,
      caption: caption,
      tag: SourceSite.byId(drama.source).name,
      badge: state != null && state.hasUpdates
          ? '${state.newEpisodes} 集更新'
          : null,
      selected: selection?.contains(drama.id),
      onTap: selection == null ? () => _open(drama) : () => _toggle(drama.id),
    );
  }

  bool _fits(BuildContext context, String text, double width) {
    final painter = TextPainter(
      text: TextSpan(
        text: text,
        style: DefaultTextStyle.of(context).style.copyWith(
          fontSize: AppScope.of(context).television ? 13 : 12,
          height: 1.3,
        ),
      ),
      textScaler: MediaQuery.textScalerOf(context),
      textDirection: TextDirection.ltr,
      maxLines: 1,
    )..layout();
    final fits = painter.width <= width - 4;
    painter.dispose();
    return fits;
  }
}
