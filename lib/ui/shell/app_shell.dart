import 'dart:async';
import 'dart:ui' show ImageFilter;

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../download_converter.dart';
import '../../download_overview.dart';
import '../../follow_updater.dart';
import '../../live/live_page.dart';
import '../../local_store.dart';
import '../../models.dart';
import '../app_scope.dart';
import '../explore/explore_page.dart';
import '../explore/source_actions.dart';
import '../downloads/downloads_page.dart';
import '../follow/follow_page.dart';
import '../settings/settings_page.dart';
import '../widgets/bar_button.dart';
import '../widgets/focus_ring.dart';
import '../widgets/source_bar.dart';

enum AppTab {
  explore('发现', Icons.explore_outlined, Icons.explore),
  follow('追剧', Icons.bookmark_border_rounded, Icons.bookmark_rounded),
  live('直播', Icons.live_tv_outlined, Icons.live_tv_rounded),
  settings('设置', Icons.settings_outlined, Icons.settings_rounded);

  const AppTab(this.label, this.icon, this.selectedIcon);
  final String label;
  final IconData icon;
  final IconData selectedIcon;
}

Future<T?> openPage<T>(BuildContext context, WidgetBuilder builder) =>
    Navigator.of(
      context,
      rootNavigator: switch (AppScope.formFactorOf(context)) {
        FormFactor.phone || FormFactor.television => true,
        FormFactor.pad || FormFactor.window => false,
      },
    ).push(MaterialPageRoute<T>(builder: builder));

class AppShell extends StatefulWidget {
  const AppShell({super.key, required this.store});
  final LocalStore store;

  @override
  State<AppShell> createState() => _AppShellState();
}

class _AppShellState extends State<AppShell>
    with SingleTickerProviderStateMixin {
  /// 底部 tab 切换过渡：与首页左右滑分类同一套视觉语言（easeOutCubic、
  /// 方向性滑入 + 淡入 + 轻微缩放），避免 IndexedStack 直接换页像卡了一下。
  /// 初始停在完成态，切换时 forward(from: 0) 重播 320ms。
  late final AnimationController _tabAnim = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 320),
    value: 1.0,
  );
  int _tabDirection = 1;

  final _navigators = [
    for (final _ in AppTab.values) GlobalKey<NavigatorState>(),
  ];
  final _tabFocus = [for (final _ in AppTab.values) FocusNode()];
  final _followEntry = FocusNode();
  final _mineEntry = FocusNode();
  final _header = FocusNode();
  final _televisionTabs = GlobalKey();
  final _explore = GlobalKey<ExplorePageState>();

  /// 底栏容器，用于把长按判定范围铺到整条底栏（图标 + 文字）。
  final _navBarKey = GlobalKey();
  var _tab = AppTab.explore;
  var _headerFocused = false;
  FocusNode? _lastFocus;

  /// 首页「再按一次返回退出应用」的倒计时窗口；非空时下一次返回键才真正退出。
  Timer? _exitWindow;

  /// 底栏「发现」长按刷新的计时状态。
  Timer? _navHold;
  Offset? _navAnchor;
  DownloadOverview? _downloads;
  DownloadConverter? _converter;
  FollowUpdater? _followUpdater;

  NavigatorState? get _navigator => _navigators[_tab.index].currentState;

  @override
  void initState() {
    super.initState();
    FocusManager.instance.addListener(_followFocus);
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _downloads ??= DownloadOverview(
      AppScope.of(context).repository,
      widget.store,
    );
    _converter ??= DownloadConverter(
      AppScope.of(context).repository,
      _downloads!,
    );
    _followUpdater ??= FollowUpdater(
      AppScope.of(context).repository,
      widget.store,
    )..start();
  }

  @override
  void dispose() {
    _tabAnim.dispose();
    FocusManager.instance.removeListener(_followFocus);
    for (final node in _tabFocus) {
      node.dispose();
    }
    _followEntry.dispose();
    _mineEntry.dispose();
    _header.dispose();
    _exitWindow?.cancel();
    _navHold?.cancel();
    _converter?.dispose();
    _downloads?.dispose();
    _followUpdater?.dispose();
    super.dispose();
  }

  void _select(AppTab tab) {
    if (tab == _tab) {
      // 已在「发现」时再单击一次图标 = 平滑回到顶部。
      if (tab == AppTab.explore) _scrollExploreToTop();
      return;
    }
    // 按 tab 顺序决定新页从左侧还是右侧滑入，和首页滑分类的方向感一致。
    _tabDirection = tab.index > _tab.index ? 1 : -1;
    _tabAnim.forward(from: 0);
    setState(() => _tab = tab);
  }

  /// 只包裹绘制层：IndexedStack 仍保留各 tab 独立的 Navigator 栈与页面状态，
  /// 动画期间新页从反侧滑入 22px、淡入并轻微放大回原尺寸，底栏与顶栏不动。
  Widget _animatedPages(Widget child) => AnimatedBuilder(
    animation: _tabAnim,
    child: child,
    builder: (context, pages) {
      final t = Curves.easeOutCubic.transform(_tabAnim.value);
      final slide = (1 - t) * 22.0 * _tabDirection;
      return Transform.translate(
        offset: Offset(slide, 0),
        child: Transform.scale(
          scale: 0.99 + 0.01 * t,
          child: Opacity(opacity: 0.6 + 0.4 * t, child: pages),
        ),
      );
    },
  );

  void _followFocus() {
    final previous = _lastFocus;
    final focused = _lastFocus = FocusManager.instance.primaryFocus;
    final index = _tabFocus.indexWhere((node) => node == focused);
    if (index < 0) return;
    if (previous?.ancestors.contains(_header) ?? false) {
      _select(AppTab.values[index]);
    } else if (index != _tab.index) {
      _tabFocus[_tab.index].requestFocus();
    }
  }

  Future<void> _back() async {
    final navigator = _navigator;
    if (navigator != null && await navigator.maybePop()) return;
    if (!mounted) return;
    final television = AppScope.of(context).television;
    // 已经在首页（且电视端焦点在顶栏）时不再「退出就完事」：先给一次提示，
    // 2 秒内再按一次才真的退出，避免遥控器按错一下就关掉应用。
    if (_tab == AppTab.explore && (!television || _headerFocused)) {
      _armExit();
      return;
    }
    // 切回首页时不再强制刷新列表。
    //
    // 原先这里会调 refresh() 绕过缓存重拉第一页，理由是「回来能看到最新内容」。
    // 但对 91porn 这类每次请求排序都不同的站源，重拉一次就等于整页重排：
    // 用户刚看过的条目会被挤到下面，主观感受就是「明明看的是 A，回来位置全变了」。
    // 首页数据本来就是缓存 + 15 分钟 TTL，需要最新内容时用户长按底栏「发现」
    // 或下拉即可主动刷新，不必在切 tab 时偷偷替换列表。
    _select(AppTab.explore);
    if (television) _tabFocus.first.requestFocus();
  }

  void _armExit() {
    const window = Duration(seconds: 2);
    setState(() {
      _exitWindow = Timer(window, () {
        if (mounted) setState(() => _exitWindow = null);
      });
    });
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(
        const SnackBar(content: Text('再按一次返回退出应用'), duration: window),
      );
  }

  void _openDownloads(BuildContext context) => openPage<void>(
    context,
    (_) => DownloadsPage(
      store: widget.store,
      downloads: _downloads!,
      converter: _converter!,
    ),
  );

  Widget _root(AppTab tab) => switch (tab) {
    AppTab.explore => ExplorePage(
      key: _explore,
      store: widget.store,
      onOpenDownloads: () => _openDownloads(context),
    ),
    AppTab.follow => FollowPage(
      store: widget.store,
      entryFocus: _followEntry,
      onExitUp: _tabFocus[AppTab.follow.index].requestFocus,
    ),
    AppTab.live => LivePage(
      repository: sharedLiveRepository(),
      store: liveStoreFor(widget.store),
      bottomInset: AppScope.formFactorOf(context) == FormFactor.phone ? 88 : 0,
    ),
    AppTab.settings => SettingsPage(store: widget.store),
  };

  @override
  Widget build(BuildContext context) {
    final pages = _animatedPages(
      NotificationListener<NavigationNotification>(
        onNotification: (_) {
          setState(() {});
          return false;
        },
        child: IndexedStack(
          index: _tab.index,
          children: [
            for (final tab in AppTab.values)
              Navigator(
                key: _navigators[tab.index],
                routeTraversalEdgeBehavior: TraversalEdgeBehavior.parentScope,
                routeDirectionalTraversalEdgeBehavior:
                    TraversalEdgeBehavior.parentScope,
                onGenerateInitialRoutes: (_, _) => [
                  MaterialPageRoute<void>(builder: (_) => _root(tab)),
                ],
              ),
          ],
        ),
      ),
    );
    final television = AppScope.of(context).television;
    final covered = _navigator?.canPop() ?? false;
    return PopScope(
      canPop:
          _exitWindow != null &&
          _tab == AppTab.explore &&
          !covered &&
          (!television || _headerFocused),
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _back();
      },
      child: switch (AppScope.formFactorOf(context)) {
        FormFactor.phone => Scaffold(
          extendBody: true,
          body: pages,
          bottomNavigationBar: covered
              ? null
              : ClipRect(
                  child: BackdropFilter(
                    filter: ImageFilter.blur(sigmaX: 24, sigmaY: 24),
                    child: Listener(
                      key: _navBarKey,
                      onPointerDown: _navPointerDown,
                      onPointerMove: _navPointerMove,
                      onPointerUp: (_) => _cancelNavHold(),
                      onPointerCancel: (_) => _cancelNavHold(),
                      child: NavigationBar(
                        selectedIndex: _tab.index,
                        onDestinationSelected: (index) =>
                            _select(AppTab.values[index]),
                        destinations: [
                          for (final tab in AppTab.values)
                            NavigationDestination(
                              key: ValueKey('nav-${tab.name}'),
                              icon: _navIcon(tab, tab.icon),
                              selectedIcon: _navIcon(tab, tab.selectedIcon),
                              label: tab.label,
                            ),
                        ],
                      ),
                    ),
                  ),
                ),
        ),
        FormFactor.pad || FormFactor.window => _wide(pages),
        FormFactor.television => Scaffold(
          body: SafeArea(
            child: Column(
              children: [
                if (!covered)
                  Padding(
                    padding: const EdgeInsets.fromLTRB(
                      televisionInset,
                      27,
                      televisionInset,
                      8,
                    ),
                    child: Focus(
                      focusNode: _header,
                      canRequestFocus: false,
                      skipTraversal: true,
                      onFocusChange: (focused) =>
                          setState(() => _headerFocused = focused),
                      onKeyEvent: (_, event) {
                        final entry = switch (_tab) {
                          AppTab.follow => _followEntry,
                          AppTab.explore => null,
                          _ => null,
                        };
                        if (event is KeyUpEvent ||
                            event.logicalKey != LogicalKeyboardKey.arrowDown ||
                            entry?.context == null) {
                          return KeyEventResult.ignored;
                        }
                        revealFocus(entry!);
                        return KeyEventResult.handled;
                      },
                      child: _televisionHeader(),
                    ),
                  ),
                Expanded(child: pages),
              ],
            ),
          ),
        ),
      },
    );
  }

  Widget _badged(AppTab tab, Widget icon) => tab == AppTab.settings
      ? _DownloadBadge(downloads: _downloads!, child: icon)
      : icon;

  /// 图标本身只负责显示，长按刷新统一由底栏 Listener 判定。
  Widget _navIcon(AppTab tab, IconData icon) => _badged(tab, Icon(icon));

  /// 底栏长按刷新：判定范围铺满整条底栏的「发现」项（图标 + 文字），
  /// 而不是只有 24px 的图标本身。只旁听指针事件，不参与手势竞技，
  /// 所以单击选中、滑动切换等原有行为完全不受影响。
  void _navPointerDown(PointerDownEvent event) {
    _cancelNavHold();
    final box = _navBarKey.currentContext?.findRenderObject();
    if (box is! RenderBox || !box.hasSize || box.size.width <= 0) return;
    final slot = box.size.width / AppTab.values.length;
    final index = (event.localPosition.dx / slot).floor();
    if (index < 0 || index >= AppTab.values.length) return;
    if (AppTab.values[index] != AppTab.explore) return;
    _navAnchor = event.position;
    _navHold = Timer(const Duration(milliseconds: 520), () {
      _navHold = null;
      _navAnchor = null;
      _refreshExplore();
    });
  }

  /// 手指在长按判定期间明显移动（滑动底栏）就取消，避免误触。
  void _navPointerMove(PointerMoveEvent event) {
    final anchor = _navAnchor;
    if (anchor == null) return;
    if ((event.position - anchor).distance > 14) _cancelNavHold();
  }

  void _cancelNavHold() {
    _navHold?.cancel();
    _navHold = null;
    _navAnchor = null;
  }

  void _refreshExplore() {
    unawaited(HapticFeedback.mediumImpact());
    final state = _explore.currentState;
    if (state != null) unawaited(state.refresh());
  }

  void _scrollExploreToTop() {
    final state = _explore.currentState;
    if (state == null) return;
    if (state.scrollOffset <= 0.5) return;
    unawaited(HapticFeedback.selectionClick());
    unawaited(state.scrollToTop());
  }

  Widget _televisionHeader() {
    final tabs = Row(
      key: _televisionTabs,
      spacing: 6,
      children: [
        for (final tab in AppTab.values)
          _TelevisionTab(
            tab: tab,
            selected: tab == _tab,
            focusNode: _tabFocus[tab.index],
            onTap: () => _select(tab),
          ),
      ],
    );
    final store = widget.store;
    if (_tab != AppTab.explore || store.source.isEmpty) {
      return SizedBox(height: 48, child: tabs);
    }
    final source = SourceSite.byId(store.source);
    return SourceBar(
      source: source.name,
      televisionLeading: tabs,
      onSwitchSource: (anchor) => pickSource(anchor, store),
      onOpenSearch: () => openSearch(context, store, source),
      onSearchSubmitted: (query) =>
          openSearch(context, store, source, query: query),
    );
  }

  Widget _wide(Widget pages) {
    final roomy = MediaQuery.sizeOf(context).width >= 840;
    final expanded = roomy && widget.store.sideBarExpanded;
    return Scaffold(
      body: Row(
        children: [
          _SideBar(
            selected: _tab,
            expanded: expanded,
            badged: _badged,
            onSelect: _select,
            onToggle: roomy
                ? () => widget.store.setSideBarExpanded(!expanded)
                : null,
          ),
          Expanded(
            child: MediaQuery.removePadding(
              context: context,
              removeLeft: true,
              child: pages,
            ),
          ),
        ],
      ),
    );
  }
}

typedef _SideMetrics = ({
  double width,
  double inset,
  double top,
  double menuGap,
  double spacing,
  double itemHeight,
  double iconSize,
  double labelSize,
  EdgeInsets labelPadding,
});

_SideMetrics _sideMetrics({required bool desktop, required bool expanded}) =>
    desktop
    ? (
        width: expanded ? 240 : 80,
        inset: 12,
        top: 12,
        menuGap: 8,
        spacing: 4,
        itemHeight: 48,
        iconSize: 22,
        labelSize: 14,
        labelPadding: const EdgeInsets.fromLTRB(17, 0, 16, 0),
      )
    : (
        width: expanded ? 232 : 88,
        inset: 16,
        top: 8,
        menuGap: 12,
        spacing: 4,
        itemHeight: 56,
        iconSize: 24,
        labelSize: 15,
        labelPadding: const EdgeInsets.fromLTRB(16, 0, 20, 0),
      );

class _SideBar extends StatelessWidget {
  const _SideBar({
    required this.selected,
    required this.expanded,
    required this.badged,
    required this.onSelect,
    this.onToggle,
  });
  final AppTab selected;
  final bool expanded;
  final Widget Function(AppTab tab, Widget icon) badged;
  final ValueChanged<AppTab> onSelect;
  final VoidCallback? onToggle;

  @override
  Widget build(BuildContext context) {
    final metrics = _sideMetrics(
      desktop: AppScope.formFactorOf(context) == FormFactor.window,
      expanded: expanded,
    );
    return Material(
      color: Theme.of(context).colorScheme.surfaceContainer,
      child: SafeArea(
        right: false,
        child: SizedBox(
          key: const ValueKey('side-bar'),
          width: metrics.width,
          child: Padding(
            padding: EdgeInsets.fromLTRB(
              metrics.inset,
              metrics.top,
              metrics.inset,
              12,
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              spacing: metrics.spacing,
              children: [
                Padding(
                  padding: EdgeInsets.only(bottom: metrics.menuGap),
                  child: Align(
                    alignment: Alignment.centerLeft,
                    child: SizedBox(
                      width: 56,
                      height: 48,
                      child: onToggle == null
                          ? null
                          : Center(
                              child: BarButton(
                                icon: Icons.menu_rounded,
                                tooltip: expanded ? '收起侧栏' : '展开侧栏',
                                onPressed: onToggle,
                              ),
                            ),
                    ),
                  ),
                ),
                for (final tab in AppTab.values)
                  _SideItem(
                    tab: tab,
                    selected: tab == selected,
                    expanded: expanded,
                    metrics: metrics,
                    badged: badged,
                    onTap: () => onSelect(tab),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _SideItem extends StatelessWidget {
  const _SideItem({
    required this.tab,
    required this.selected,
    required this.expanded,
    required this.metrics,
    required this.badged,
    required this.onTap,
  });
  final AppTab tab;
  final bool selected;
  final bool expanded;
  final _SideMetrics metrics;
  final Widget Function(AppTab tab, Widget icon) badged;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    final color = selected
        ? colors.onSecondaryContainer
        : colors.onSurfaceVariant;
    final icon = badged(
      tab,
      Icon(
        selected ? tab.selectedIcon : tab.icon,
        color: color,
        size: metrics.iconSize,
      ),
    );
    final item = Material(
      color: selected ? colors.secondaryContainer : Colors.transparent,
      shape: const StadiumBorder(),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        key: ValueKey('nav-${tab.name}'),
        onTap: onTap,
        child: SizedBox(
          height: metrics.itemHeight,
          child: expanded
              ? Padding(
                  padding: metrics.labelPadding,
                  child: Row(
                    spacing: 12,
                    children: [
                      icon,
                      Expanded(
                        child: Text(
                          tab.label,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.titleSmall?.copyWith(
                            fontSize: metrics.labelSize,
                            fontWeight: selected
                                ? FontWeight.w700
                                : FontWeight.w500,
                            color: color,
                          ),
                        ),
                      ),
                    ],
                  ),
                )
              : Center(child: icon),
        ),
      ),
    );
    return Semantics(
      selected: selected,
      child: FocusRing(
        radius: metrics.itemHeight / 2,
        child: expanded ? item : Tooltip(message: tab.label, child: item),
      ),
    );
  }
}

class _DownloadBadge extends StatelessWidget {
  const _DownloadBadge({required this.downloads, required this.child});
  final DownloadOverview downloads;
  final Widget child;

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: downloads,
    builder: (context, child) {
      final count = downloads.active;
      return Badge(
        isLabelVisible: count > 0,
        label: Text(count > 99 ? '99+' : '$count'),
        child: child,
      );
    },
    child: child,
  );
}

class _TelevisionTab extends StatelessWidget {
  const _TelevisionTab({
    required this.tab,
    required this.selected,
    required this.focusNode,
    required this.onTap,
  });
  final AppTab tab;
  final bool selected;
  final FocusNode focusNode;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Semantics(
      selected: selected,
      child: FocusPill(
        focusNode: focusNode,
        selected: selected,
        textStyle: theme.textTheme.titleMedium?.copyWith(
          fontSize: 16,
          fontWeight: selected ? FontWeight.w700 : FontWeight.w500,
          color: theme.colorScheme.onSurfaceVariant,
        ),
        child: InkWell(
          key: ValueKey('nav-${tab.name}'),
          focusNode: focusNode,
          autofocus: tab == AppTab.explore,
          focusColor: Colors.transparent,
          onTap: onTap,
          child: Container(
            height: 40,
            padding: const EdgeInsets.symmetric(horizontal: 18),
            alignment: Alignment.center,
            child: Text(tab.label),
          ),
        ),
      ),
    );
  }
}
