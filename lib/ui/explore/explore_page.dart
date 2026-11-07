import 'dart:async';

import 'package:flutter/material.dart';

import '../../explore_feed.dart';
import '../../local_store.dart';
import '../../models.dart';
import '../app_scope.dart';
import '../shell/app_shell.dart';
import '../detail/detail_page.dart';
import '../widgets/bar_button.dart';
import '../widgets/category_tabs.dart';
import '../widgets/choice_panel.dart';
import '../widgets/drama_grid.dart';
import '../widgets/frosted_header.dart';
import '../widgets/page_header.dart';
import '../widgets/source_bar.dart';
import '../widgets/status_view.dart';
import 'explore_hero.dart';
import 'source_actions.dart';

class ExplorePage extends StatefulWidget {
  const ExplorePage({super.key, required this.store});
  final LocalStore store;

  @override
  State<ExplorePage> createState() => ExplorePageState();
}

class ExplorePageState extends State<ExplorePage>
    with SingleTickerProviderStateMixin {
  ExploreFeed? _explore;
  final _scroll = ScrollController();
  final _search = TextEditingController();
  final _tabsKey = GlobalKey();
  String _shown = '';

  /// 每个分类（source|tab）各自记住滚动位置，切走再切回来时回到原处。
  final Map<String, double> _offsets = {};
  double? _restoreTarget;
  String? _hintText;
  IconData _hintIcon = Icons.swap_horiz_rounded;
  Timer? _hintTimer;

  /// 切分类时的内容过渡动画：初始停在完成态（value = 1），切换时
  /// forward(from: 0) 重播 340ms 的滑入 + 淡入。
  late final AnimationController _switchAnim = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 340),
    value: 1.0,
  );
  int _switchDirection = 1;
  double _dragX = 0;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _explore ??= ExploreFeed(AppScope.of(context).repository, widget.store);
  }

  @override
  void dispose() {
    _hintTimer?.cancel();
    _switchAnim.dispose();
    _explore?.dispose();
    _scroll.dispose();
    _search.dispose();
    super.dispose();
  }

  Future<void> refresh() async {
    _restoreTarget = null;
    _offsets[_shown] = 0;
    if (_scroll.hasClients) _scroll.jumpTo(0);
    _showHint('正在刷新…', Icons.sync_rounded, hold: const Duration(seconds: 10));
    try {
      await _explore?.refresh();
      if (!mounted) return;
      // 列表请求失败时 DramaFeed 不抛异常，只把错误记在 error 上，
      // 所以这里要读一次，避免明明刷挂了还提示「已刷新」。
      final failed = (_explore?.feed?.error ?? '').isNotEmpty;
      _showHint(
        failed ? '刷新失败，稍后再试' : '已刷新',
        failed ? Icons.error_outline_rounded : Icons.check_circle_outline_rounded,
        hold: const Duration(milliseconds: 1400),
      );
    } catch (_) {
      if (!mounted) return;
      _showHint(
        '刷新失败，稍后再试',
        Icons.error_outline_rounded,
        hold: const Duration(milliseconds: 1800),
      );
    }
  }

  /// 当前滚动偏移，供底栏「发现」单击判断是否已经在顶部。
  double get scrollOffset => _scroll.hasClients ? _scroll.offset : 0;

  /// 底栏「发现」单击：平滑回到当前分类顶部。
  /// 同时把该分类记住的位置归零，避免切走再切回来又跳回原处。
  Future<void> scrollToTop() async {
    _restoreTarget = null;
    if (_shown.isNotEmpty) _offsets[_shown] = 0;
    if (!_scroll.hasClients || _scroll.offset <= 0.5) return;
    try {
      await _scroll.animateTo(
        0,
        duration: const Duration(milliseconds: 320),
        curve: Curves.easeOutCubic,
      );
    } catch (_) {
      // 动画途中页面被销毁或控制器解绑，忽略即可。
    }
  }

  /// 主屏左右快滑切换相邻分类：左滑下一个、右滑上一个，到头即停。
  /// 分类栏会自动滚动到新选中的标签，内容由 ExploreFeed 重新加载。
  void _switchTabBy(ExploreFeed explore, int delta) {
    final tabs = explore.tabs;
    if (tabs.length < 2) return;
    final index = tabs.indexWhere((tab) => tab.id == explore.tab.id);
    if (index < 0) return;
    final next = index + delta;
    if (next < 0 || next >= tabs.length) return;
    // 记录方向并启动过渡动画，再换分类：新内容从滑动反侧滑入淡入，
    // 而不是整屏一瞬间重建，看起来像卡了一下。
    _switchDirection = delta > 0 ? 1 : -1;
    _switchAnim.forward(from: 0);
    explore.select(tabs[next].id);
    _showTabHint(tabs[next].name);
  }

  void _showTabHint(String name) =>
      _showHint(name, Icons.swap_horiz_rounded);

  /// 顶部中央浮出的玻璃提示胶囊（切分类、刷新状态共用）。
  void _showHint(String text, IconData icon, {Duration? hold}) {
    _hintTimer?.cancel();
    setState(() {
      _hintText = text;
      _hintIcon = icon;
    });
    _hintTimer = Timer(hold ?? const Duration(milliseconds: 1100), () {
      if (mounted) setState(() => _hintText = null);
    });
  }

  /// 内容区的滑动层：横向手势切分类，中央上方浮出当前分类的玻璃提示。
  /// 用 translucent 命中，不拦截卡片点击与网格的垂直滚动；电视端靠遥控器
  /// 方向键，不叠加手势。Hero 轮播自身的 PageView 会优先消费横向滑动，
  /// 所以在轮播上仍是翻海报，不会误切分类。
  Widget _swipeLayer(ExploreFeed explore, Widget child, {double hintTop = 12}) {
    if (AppScope.formFactorOf(context) == FormFactor.television) return child;
    return GestureDetector(
      // 拖动时内容带阻尼轻微跟手，松手后由切换动画接管，手感更连贯。
      onHorizontalDragUpdate: (details) {
        final next = (_dragX + details.delta.dx * 0.10).clamp(-18.0, 18.0);
        if (next != _dragX) setState(() => _dragX = next);
      },
      onHorizontalDragEnd: (details) {
        final velocity = details.primaryVelocity ?? 0;
        setState(() => _dragX = 0);
        if (velocity.abs() < 200) return;
        _switchTabBy(explore, velocity < 0 ? 1 : -1);
      },
      onHorizontalDragCancel: () => setState(() => _dragX = 0),
      behavior: HitTestBehavior.translucent,
      child: Stack(
        children: [
          Positioned.fill(
            child: AnimatedBuilder(
              animation: _switchAnim,
              child: child,
              builder: (context, content) {
                final t = Curves.easeOutCubic.transform(_switchAnim.value);
                final slide = (1 - t) * 26.0 * _switchDirection;
                return Transform.translate(
                  offset: Offset(_dragX + slide, 0),
                  child: Opacity(opacity: 0.55 + 0.45 * t, child: content),
                );
              },
            ),
          ),
          Positioned(top: hintTop, left: 0, right: 0, child: _swipeHintBadge()),
        ],
      ),
    );
  }

  /// 手机端磨砂顶栏：与底栏 NavigationBar 用同一套「内容延伸到底 +
  /// BackdropFilter 模糊透出」的逻辑。顶栏浮在内容之上，内容滚动时
  /// 从它背后经过并被模糊，而不是被一条实心横条挡在下方。
  Widget _frostedHeader(Widget child) => FrostedHeader(child: child);

  Widget _swipeHintBadge() => IgnorePointer(
    child: Center(
      child: AnimatedOpacity(
        opacity: _hintText == null ? 0 : 1,
        duration: const Duration(milliseconds: 180),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 9),
          decoration: BoxDecoration(
            color: Colors.black.withValues(alpha: 0.62),
            borderRadius: BorderRadius.circular(22),
            border: Border.all(color: Colors.white.withValues(alpha: 0.24)),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                _hintIcon,
                size: 15,
                color: Colors.white.withValues(alpha: 0.8),
              ),
              const SizedBox(width: 7),
              Text(
                _hintText ?? '',
                style: const TextStyle(
                  fontSize: 14,
                  fontWeight: FontWeight.w800,
                  letterSpacing: -0.2,
                  color: Colors.white,
                ),
              ),
            ],
          ),
        ),
      ),
    ),
  );

  void _open(Drama drama) => openPage<void>(
    context,
    (_) => DetailPage(store: widget.store, drama: drama),
  );

  Future<void> _pickCategory(BuildContext anchor) async {
    final explore = _explore!;
    final value = await showChoicePanel<String>(
      context: anchor,
      title: '全部分类',
      options: [for (final tab in explore.tabs) ChoiceOption(tab.id, tab.name)],
      selected: explore.tab.id,
      grid: ChoiceGrid.categories,
    );
    if (value != null) explore.select(value);
  }

  void _searchSubmitted(SourceSite source, String query) {
    if (query.trim().isEmpty) return;
    _search.clear();
    openSearch(context, widget.store, source, query: query);
  }

  void _resetScroll(ExploreFeed explore) {
    final shown = '${explore.source?.id}|${explore.tab.id}';
    if (shown == _shown) return;
    final previous = _shown;
    _shown = shown;
    // 离开旧分类时先把它滑到的位置存起来，切回来再还原；
    // 从没去过的分类仍然从顶部开始。
    if (previous.isNotEmpty && _scroll.hasClients) {
      _offsets[previous] = _scroll.offset;
    }
    _restoreTarget = _offsets[shown] ?? 0;
  }

  /// 等新分类的内容就绪后，把滚动位置还原到上次离开的地方。
  /// 内容还在加载时先不跳，等下一次重建（列表填好后）再恢复。
  void _applyRestore(ExploreFeed explore) {
    final target = _restoreTarget;
    if (target == null) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || _restoreTarget != target || !_scroll.hasClients) return;
      final max = _scroll.position.maxScrollExtent;
      if (target == 0) {
        _scroll.jumpTo(0);
        _restoreTarget = null;
        return;
      }
      final feed = explore.feed;
      final ready = feed != null && (feed.items.isNotEmpty || !feed.loading);
      if (ready || max >= target) {
        _scroll.jumpTo(target.clamp(0.0, max));
        _restoreTarget = null;
      }
    });
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: _explore!,
    builder: (context, _) {
      final explore = _explore!;
      final source = explore.source;
      final form = AppScope.formFactorOf(context);
      final television = form == FormFactor.television;
      final desktop = form == FormFactor.window;
      _resetScroll(explore);
      _applyRestore(explore);
      if (source == null) {
        return Scaffold(
          body: SafeArea(
            bottom: false,
            child: Column(
              children: [
                if (!television) const PageHeader(title: '发现'),
                const Expanded(
                  child: StatusView(
                    title: '暂无可用站源',
                    message: '请在站源管理中开启站源，或联系管理员开放权限。',
                    icon: Icons.public_off_rounded,
                  ),
                ),
              ],
            ),
          ),
        );
      }
      final inset = switch (form) {
        FormFactor.phone => 16.0,
        FormFactor.pad || FormFactor.window => 24.0,
        FormFactor.television => televisionInset,
      };
      final tabs = Padding(
        padding: EdgeInsets.symmetric(
          horizontal: form == FormFactor.phone ? 0 : inset,
        ),
        child: CategoryTabs<String>(
          key: _tabsKey,
          options: [
            for (final tab in explore.tabs) ChoiceOption(tab.id, tab.name),
          ],
          selected: explore.tab.id,
          onSelected: explore.select,
          // 「全部分类」固定在分类栏右端，与标签栏对齐，随时可点。
          onShowAll: _pickCategory,
        ),
      );
      final header = SourceBar(
        source: source.name,
        searchController: _search,
        onSwitchSource: (anchor) => pickSource(anchor, widget.store),
        onOpenSearch: () => openSearch(context, widget.store, source),
        onSearchSubmitted: (query) => _searchSubmitted(source, query),
        actions: [
          // 直播已移到底部导航作为独立 tab，顶栏不再重复放入口。
          if (desktop)
            BarButton(
              icon: Icons.refresh_rounded,
              tooltip: '刷新',
              onPressed: explore.refresh,
            ),
        ],
      );
      final showTabs = !television || explore.feed!.items.isEmpty;
      // 手机端：顶栏浮在内容之上做磨砂，内容延伸到屏幕顶（含状态栏），
      // 初始用留白让开顶栏；滚动后卡片从顶栏背后经过并被模糊透出，
      // 与底栏 NavigationBar 的「内容透过模糊」是同一套逻辑。
      if (form == FormFactor.phone) {
        final topPad = MediaQuery.paddingOf(context).top;
        const headerBody = 62.0 + 48.0 + 8.0; // 标题栏 + 分类栏 + 余量
        final topInset = topPad + headerBody;
        return Scaffold(
          body: Stack(
            children: [
              Positioned.fill(
                child: _swipeLayer(
                  explore,
                  _content(explore, inset, false, tabs, topInset: topInset),
                  hintTop: topInset + 4,
                ),
              ),
              Positioned(
                top: 0,
                left: 0,
                right: 0,
                child: _frostedHeader(
                  SafeArea(
                    bottom: false,
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [header, if (showTabs) tabs],
                    ),
                  ),
                ),
              ),
            ],
          ),
        );
      }
      return Scaffold(
        body: SafeArea(
          bottom: false,
          child: Column(
            children: [
              if (!television) header,
              if (showTabs) tabs,
              Expanded(
                child: _swipeLayer(
                  explore,
                  _content(explore, inset, television, tabs),
                ),
              ),
            ],
          ),
        ),
      );
    },
  );

  Widget _content(
    ExploreFeed explore,
    double inset,
    bool television,
    Widget tabs, {
    // 手机端顶栏是浮层时，内容首部留出的空白，避免初始内容被磨砂顶栏压住。
    double topInset = 0,
  }) {
    final feed = explore.feed!;
    final hero = _hero(explore);
    final items = explore.items;
    if (feed.items.isEmpty) {
      final Widget status;
      if (feed.loading) {
        status = const StatusView.loading();
      } else if (feed.error.isNotEmpty) {
        status = StatusView(
          title: '暂时无法加载',
          message: feed.error,
          icon: Icons.cloud_off_rounded,
          action: '重试',
          onAction: () => feed.reload(force: true),
        );
      } else {
        status = StatusView(
          title: '这里还没有剧',
          message: '换一个分类看看',
          icon: Icons.inbox_outlined,
          action: '刷新',
          onAction: () => feed.reload(force: true),
        );
      }
      return topInset > 0
          ? Padding(
              padding: EdgeInsets.only(top: topInset),
              child: status,
            )
          : status;
    }
    final phone = AppScope.formFactorOf(context) == FormFactor.phone;
    final desktop = AppScope.formFactorOf(context) == FormFactor.window;
    return LayoutBuilder(
      builder: (context, constraints) {
        final scroll = CustomScrollView(
          controller: _scroll,
          physics: const AlwaysScrollableScrollPhysics(),
          slivers: [
            // 顶栏留白：随内容一起滚动，滚动后卡片进入顶栏背后被模糊透出。
            if (topInset > 0)
              SliverToBoxAdapter(child: SizedBox(height: topInset)),
            if (television)
              SliverPadding(
                padding: const EdgeInsets.only(top: 8),
                sliver: SliverToBoxAdapter(child: tabs),
              ),
            if (hero != null)
              SliverPadding(
                padding: EdgeInsets.fromLTRB(inset, phone ? 8 : 16, inset, 0),
                sliver: SliverToBoxAdapter(child: hero),
              ),
            SliverPadding(
              padding: EdgeInsets.fromLTRB(inset, desktop ? 20 : 16, inset, 0),
              sliver: DramaSliverGrid(
                width: constraints.maxWidth - inset * 2,
                items: items,
                rankOf: explore.rankOf,
                autofocus: television && hero == null,
                onOpen: _open,
              ),
            ),
            SliverPadding(
              padding: EdgeInsets.fromLTRB(
                inset,
                8,
                inset,
                // 底部留出悬浮胶囊导航的空间。
                television ? 27 : (phone ? 100 : 16),
              ),
              sliver: SliverToBoxAdapter(child: FeedFooter(feed: feed)),
            ),
          ],
        );
        return Stack(
          children: [
            AutoLoadMore(
              feed: feed,
              child: television
                  ? scroll
                  : RefreshIndicator(onRefresh: explore.refresh, child: scroll),
            ),
            if (feed.loading)
              const Positioned(
                left: 0,
                right: 0,
                top: 0,
                child: LinearProgressIndicator(minHeight: 2),
              ),
          ],
        );
      },
    );
  }

  Widget? _hero(ExploreFeed explore) {
    final items = explore.items;
    // 轮播取当前分类列表的前几部，每部都是真实剧集，可左右滑动查看。
    List<Drama> slides(Drama lead) =>
        [lead, ...items.where((d) => d.id != lead.id)].take(6).toList();
    if (explore.resume case final entry?) {
      final drama = entry.drama;
      final total = drama.episodes;
      final next = entry.finished && entry.episode < total
          ? entry.episode + 1
          : entry.episode;
      return ExploreHero(
        drama: drama,
        carousel: slides(drama),
        kicker: '继续观看',
        meta: total > 0
            ? '看到第 ${entry.episode} 集 · 共 $total 集'
            : '看到第 ${entry.episode} 集',
        progress: total > 0 ? entry.episode / total : null,
        action: '继续第 $next 集',
        onPlay: () => _open(drama),
        onDetail: () => _open(drama),
        onOpen: _open,
      );
    }
    if (explore.featured case final drama?) {
      return ExploreHero(
        drama: drama,
        carousel: slides(drama),
        kicker: '今日推荐',
        meta: [
          if (drama.category.isNotEmpty) drama.category,
          if (drama.episodes > 0) '共 ${drama.episodes} 集',
        ].join(' · '),
        action: '立即观看',
        onPlay: () => _open(drama),
        onDetail: () => _open(drama),
        onOpen: _open,
      );
    }
    return null;
  }
}
