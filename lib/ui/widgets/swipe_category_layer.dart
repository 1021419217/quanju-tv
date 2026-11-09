import 'dart:async';

import 'package:flutter/material.dart';

import '../app_scope.dart';

/// 与首页同款的「左右快滑切换分类」层。
///
/// 把内容区包进来，传分类数量 / 当前序号 / 分类名，快滑时回调目标序号：
/// 拖动时内容带阻尼轻微跟手，松手后由切换动画接管，顶部中央浮出当前分类
/// 的玻璃提示胶囊。用 translucent 命中，不拦截卡片点击与网格的垂直滚动；
/// 电视端靠遥控器方向键，不叠加手势——这些行为与 ExplorePage._swipeLayer
/// 完全一致，追剧 / 下载 / 直播页复用同一套手感。
class SwipeCategoryLayer extends StatefulWidget {
  const SwipeCategoryLayer({
    super.key,
    required this.itemCount,
    required this.index,
    required this.labels,
    required this.onIndexChanged,
    required this.child,
    this.hintTop = 12,
  });

  final int itemCount;
  final int index;
  final List<String> labels;
  final ValueChanged<int> onIndexChanged;
  final Widget child;
  final double hintTop;

  @override
  State<SwipeCategoryLayer> createState() => _SwipeCategoryLayerState();
}

class _SwipeCategoryLayerState extends State<SwipeCategoryLayer>
    with SingleTickerProviderStateMixin {
  late final AnimationController _anim = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 340),
    value: 1.0,
  );
  int _direction = 1;
  double _dragX = 0;
  String? _hint;
  Timer? _hintTimer;

  @override
  void dispose() {
    _hintTimer?.cancel();
    _anim.dispose();
    super.dispose();
  }

  void _switch(int delta) {
    if (widget.itemCount < 2) return;
    final next = widget.index + delta;
    if (next < 0 || next >= widget.itemCount) return;
    _direction = delta;
    _anim.forward(from: 0);
    widget.onIndexChanged(next);
    _hintTimer?.cancel();
    setState(() => _hint = next < widget.labels.length ? widget.labels[next] : '');
    _hintTimer = Timer(const Duration(milliseconds: 1100), () {
      if (mounted) setState(() => _hint = null);
    });
  }

  @override
  Widget build(BuildContext context) {
    if (widget.itemCount < 2 ||
        AppScope.formFactorOf(context) == FormFactor.television) {
      return widget.child;
    }
    return GestureDetector(
      onHorizontalDragUpdate: (details) {
        final next = (_dragX + details.delta.dx * 0.10).clamp(-18.0, 18.0);
        if (next != _dragX) setState(() => _dragX = next);
      },
      onHorizontalDragEnd: (details) {
        final velocity = details.primaryVelocity ?? 0;
        setState(() => _dragX = 0);
        if (velocity.abs() < 200) return;
        _switch(velocity < 0 ? 1 : -1);
      },
      onHorizontalDragCancel: () => setState(() => _dragX = 0),
      behavior: HitTestBehavior.translucent,
      child: Stack(
        children: [
          Positioned.fill(
            child: AnimatedBuilder(
              animation: _anim,
              child: widget.child,
              builder: (context, content) {
                final t = Curves.easeOutCubic.transform(_anim.value);
                final slide = (1 - t) * 26.0 * _direction;
                return Transform.translate(
                  offset: Offset(_dragX + slide, 0),
                  child: Opacity(opacity: 0.55 + 0.45 * t, child: content),
                );
              },
            ),
          ),
          Positioned(top: widget.hintTop, left: 0, right: 0, child: _badge()),
        ],
      ),
    );
  }

  Widget _badge() => IgnorePointer(
    child: Center(
      child: AnimatedOpacity(
        opacity: _hint == null ? 0 : 1,
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
              const Icon(
                Icons.swap_horiz_rounded,
                size: 16,
                color: Colors.white,
              ),
              const SizedBox(width: 8),
              Text(
                _hint ?? '',
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ),
        ),
      ),
    ),
  );
}
