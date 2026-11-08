import 'dart:convert';

import 'app_build.dart';

class SourceSite {
  const SourceSite(this.id, this.name, this.description);
  final String id;
  final String name;
  final String description;

  /// 影视站源（seku 引擎，v_ 前缀）支持在线搜索与翻页。
  bool get isVideoStation => id.startsWith('v_');

  bool get onlineSearch => pagedSearch || duanjuOnlineSearch;

  /// 短剧家族源都支持关键词在线搜索（与 Go 侧 duanjuSourceSpec.Searcher 对齐）。
  bool get duanjuOnlineSearch => _duanjuIds.contains(id);

  bool get pagedSearch =>
      isVideoStation ||
      id == 'fanqie' ||
      id == 'hongguo' ||
      id == 'huangju' ||
      id == 'yeguo' ||
      id == 'dsd' ||
      id == 'sorani' ||
      id == 'guipian' ||
      id == 'hanxiaoquan' ||
      id == 'ikanbot' ||
      id == 'a123' ||
      id == 'xchina' ||
      id == 'xchina-live' ||
      id == 'p91' ||
      id == 'beabox' ||
      id == 'kissav' ||
      id == 'viral' ||
      id == 'theporny' ||
      id == 'jinguoduanju' ||
      id == 'jinguo' ||
      id == 'heitao' ||
      duanjuPaged;

  /// 短剧家族里支持翻页的源（与 Go 侧 duanjuSourceSpec.Paged 对齐）。
  bool get duanjuPaged => _duanjuPagedIds.contains(id);

  bool get searchSuggestions => id == 'hongguo';
  String get groupId => switch (id) {
    'huangguo-video' || 'huangguoai' || 'cloudfront' => 'huangguo',
    _ => id,
  };
  String get groupName => groupId == 'huangguo' ? '黄果' : name;
  String get entryName => switch (id) {
    'huangguo-video' => '视频',
    'huangguoai' => 'AI',
    'cloudfront' => '旧版',
    _ => name,
  };

  static const hongguo = SourceSite('hongguo', '红果', '短剧 · 漫剧 · AI 剧');

  /// 番茄：内容来自番茄短剧，由本机内嵌的番茄内容引擎（127.0.0.1:18921）提供。
  static const fanqie = SourceSite('fanqie', '番茄', '番茄短剧 · 漫剧 · 在线搜索');

  static const dsd = SourceSite('dsd', '帝果', '分类视频 · 在线搜索');
  static const sorani = SourceSite('sorani', '青空', '番剧 · 剧场动画 · 特摄');
  static const guipian = SourceSite('guipian', '鬼片', '鬼片 · 电视剧 · 动漫');
  static const hanxiaoquan = SourceSite(
    'hanxiaoquan',
    '韩小圈',
    '韩剧 · 韩国电影 · 综艺动漫',
  );
  static const ikanbot = SourceSite('ikanbot', '爱看机器人', '电影 · 剧集 · 多线路搜索');
  static const a123 = SourceSite('a123', 'A123', '电影 · 连续剧 · 综艺 · 动漫 · 多线路');
  static const jinguoduanju = SourceSite('jinguoduanju', '禁果短剧', '推荐 · 分类短剧 · 在线搜索');
  static const jinguo = SourceSite('jinguo', '禁果', '精选 · 最新 · 分类短剧 · 在线搜索');
  static const heitao = SourceSite('heitao', '黑桃', '分类短剧 · 在线搜索');

  /// XChina 实时上新：与 XChina 同一个站，但目录固定走站方按上传时间倒序的
  /// 实时流，刷新时整体替换第一页，用来验证能不能刷出新内容。
  static const xchinaLive = SourceSite(
    'xchina-live',
    'XChina·实时',
    '最新上传 · 按时间倒序',
  );

  /// 91porn：站方页面直抓，默认「最近更新」按发布时间倒序，
  /// 与网页版同一份内容，支持站方 9 个分类与关键词搜索。
  static const p91 = SourceSite('p91', '91porn', '最近更新 · 分类 · 在线搜索');

  /// 笔盒（BeaBox）：官网 JSON 接口，响应体 AES 加密，域名会自动换，
  /// 由接口里的域名池自我修复，支持分类与关键词搜索。
  static const beabox = SourceSite('beabox', '笔盒', '最新 · 分类 · 在线搜索');

  /// KISSAV：日语 AV 聚合站，播放走站方 m3u8 代理（分片自带签名）。
  static const kissav = SourceSite('kissav', 'KISSAV', '新作 · 分类 · 在线搜索');

  /// ViralXXX：KVS 站，详情页直接给成品 mp4，CDN 公开支持 Range。
  static const viral = SourceSite('viral', 'ViralXXX', '最新 · 分类 · 在线搜索');

  /// 短剧家族内置源（全剧视界可用），与 Go 侧 duanjuProviderCatalog 对齐。
  static const duanjuValues = [
    SourceSite('yaguo', '芽果', '星芽短剧 · 登录接口'),
    SourceSite('maoguo', '猫果', '七猫短剧 · 签名接口'),
    SourceSite('fanguo', '饭果', '西饭短剧 · 搜索接口'),
    SourceSite('guanguo', '观果', '围观短剧 · 分类接口'),
    SourceSite('heguo', '河果', '河马剧场 · 网页接口'),
    SourceSite('xingguo', '星果', '星星短剧 · 连载接口'),
    SourceSite('huaguo', '花果', '花生短剧 · 网页目录'),
    SourceSite('niuguo', '牛果', '牛牛短剧 · 分类接口'),
    SourceSite('wangguo', '网果', '短剧网站 · 网页目录'),
    SourceSite('faguo', '发果', '168 短剧 · 网页目录'),
    SourceSite('wuguo', '伍果', '五五短剧 · 网页目录'),
  ];

  static const _duanjuIds = {
    'yaguo',
    'maoguo',
    'fanguo',
    'guanguo',
    'heguo',
    'xingguo',
    'huaguo',
    'niuguo',
    'wangguo',
    'faguo',
    'wuguo',
  };

  static const _duanjuPagedIds = {
    'yaguo',
    'guanguo',
    'huaguo',
    'niuguo',
    'wangguo',
    'faguo',
    'wuguo',
  };

  /// 影视站源（seku 引擎 · 移植自红蚁TV huoyi-104-source，共 73 个）
  static const videoValues = [
    SourceSite('v_missav', 'MissAV', 'MissAV 镜像 · 槽位 hongguo'),
    SourceSite('v_xvideos', 'XVideos', 'XVideos · 槽位 ikanbot'),
    SourceSite('v_hsck', '黄色仓库', 'hsck123 · 槽位 a123'),
    SourceSite('v_javmap', '日本AV大全', 'JavMap · 槽位 hanxiaoquan'),
    SourceSite('v_dayday', '天天A片', 'vjav.tube · 槽位 guipian'),
    SourceSite('v_whos', 'whos识图', 'whos.tv · 槽位 huangdou'),
    SourceSite('v_91sanji', '91三级', '91sanji · 槽位 huangju'),
    SourceSite('v_hstv', 'HSTV', 'HSTV 仅免费 · 槽位 yeguo'),
    SourceSite('v_dsd', '懂色帝', 'DSD · 槽位 dsd'),
    SourceSite('v_javmit', 'Javmit', 'javmit · 槽位 huangguo-video'),
    SourceSite('v_vjav', 'VJAV', 'vjav.com · 槽位 huangguoai'),
    SourceSite('v_qishe', '妻社', '人妻社 · 槽位 cloudfront'),
    SourceSite('v_aipornx', 'AIPornX', 'aipornx · 槽位 yaguo'),
    SourceSite('v_51xz', '51XZ', '51xz · 槽位 maoguo'),
    SourceSite('v_haijiao_fanguo', '海角社区', 'haijiao · 槽位 fanguo'),
    SourceSite('v_javher', 'JavHer', 'JavHer 预告 · 槽位 guanguo'),
    SourceSite('v_one', 'ONE', 'ONE · 槽位 heguo'),
    SourceSite('v_javhd', 'JavHD', 'javhd · 槽位 xingguo'),
    SourceSite('v_yuuhc', '欲含春', 'yuuhc · 槽位 huaguo'),
    SourceSite('v_setang', '色堂', 'setang · 槽位 niuguo'),
    SourceSite('v_javxu', 'JavXu', 'javxu · 槽位 wangguo'),
    SourceSite('v_javrate', '鉴赏师', 'javrate · 槽位 faguo'),
    SourceSite('v_99jj', '久久热', '99jj · 槽位 wuguo'),
    SourceSite('v_javmenu', 'JavMenu', 'javmenu.com'),
    SourceSite('v_pektino', 'Pektino', 'pektino.com'),
    SourceSite('v_xszav', 'XSZAV', 'cn.xsz-av.com'),
    SourceSite('v_123av', '123AV', '123av.com'),
    SourceSite('v_520av', '520AV影城', '520avtv.com'),
    SourceSite('v_pigav', 'PIGAV', 'pigav.com'),
    SourceSite('v_aoxx69', 'AOXX69', 'aoxx69.app · 亞洲綜合'),
    SourceSite('v_javdb', 'JavDB', 'JavDB 磁力'),
    SourceSite('v_z2_17rip', '17rip', '17rip.com'),
    SourceSite('v_z2_321av', '321AV', '321av.net'),
    SourceSite('v_z2_av6k', 'AV6K', 'av6k.com'),
    SourceSite('v_z2_7mm', '7MMTV', '7mmtv.sx'),
    SourceSite('v_z2_xanimu', 'Xanimu', 'xanimu.one'),
    SourceSite('v_z2_cn', '中文秘密资源', 'cnlovehubx18.xyz'),
    SourceSite('v_z2_thdh', '探花短视频', 'thdhhaj.xyz'),
    SourceSite('v_xhy11', '小黄鸭', 'xhy11.cc'),
    SourceSite('v_jack20', '後宮電影院', '20jack.com'),
    SourceSite('v_javguru', 'JavGuru', 'jav.guru'),
    SourceSite('v_sihu', '四虎', '4hu.tv'),
    SourceSite('v_tg', 'TG频道', 'tg.i-c-a.su 网关'),
    SourceSite('v_kuaihuo', '快活影院', 'tbryy · 静态站直取'),
    SourceSite('v_avbigplat', 'AV大平台', '9sex · 备用域可直达'),
    SourceSite('v_hxc', '含羞草', '加密API · 站方预告口径'),
    SourceSite('v_hjw', '海角网', 'web版 · 子域池活入口'),
    SourceSite('v_goodav', 'GoodAV', 'goodav17 · jar配方移植 · 全链实拉'),
    SourceSite('v_ikun', 'iKun资源', 'ikunzyapi · MacCMS聚合 · 全链实拉'),
    SourceSite('v_senlin', '森林资源', 'senlin聚合 · 6源MacCMS · 全链实拉'),
    SourceSite('v_km', '快猫', '欲火移植 · 待真机验'),
    SourceSite('v_lxs', '撸先生', '欲火移植 · 封面解密 · 全链实拉'),
    SourceSite('v_km2', '新快猫', '欲火移植 · 全链验通'),
    SourceSite('v_maomi', '猫咪', '欲火移植 · 待真机验'),
    SourceSite('v_fulao2', '腐老二', '欲火移植 · 令牌门待解'),
    SourceSite('v_xj', '香蕉视频', '欲火移植 · 全腿实测'),
    SourceSite('v_cucumber', '黄瓜视频', '欲火移植 · 全链验通'),
    SourceSite('v_avbobo', 'AVbobo', '欲火移植 · 内置共享账号登录'),
    SourceSite('v_bilibili', '逼哩', '欲火移植 · 待真机验'),
    SourceSite('v_fruitpie', '水果派', '欲火移植 · 全链验通'),
    SourceSite('v_melon', 'melon', '欲火移植 · 全链验通'),
    SourceSite('v_hl365', '黑料不打烊', '欲火移植 · 全链验通'),
    SourceSite('v_pron91', '91pron', '欲火移植 · 全链验通'),
    SourceSite('v_short91', '91短视频', '欲火移植 · 全链验通'),
    SourceSite('v_melon51', '51西瓜', '欲火移植 · 全链验通'),
    SourceSite('v_utube', 'utube', '欲火移植 · 全链验通'),
    SourceSite('v_av91', 'av91', '欲火移植 · 免费腿全链验通'),
    SourceSite('v_md', '麻豆', '欲火移植 · 全链验通'),
    SourceSite('v_haijiao', '海角社区·移植版', '欲火移植 · 除搜索封面外全验'),
    SourceSite('v_sex8', '杏吧视频·夜杏有声', '欲火移植 · 全链验通 · 无搜索'),
    SourceSite('v_gdian', 'G点短视频', '欲火移植 · 待真机验'),
    SourceSite('v_ins', 'INS社区', '欲火移植 · 待真机验'),
    SourceSite('v_huangguo', '黄果', '欲火移植 · 全腿实测'),
  ];

  static const primaryValues = [
    fanqie,
    hongguo,
    a123,
    ikanbot,
    hanxiaoquan,
    guipian,
    sorani,
    ...duanjuValues,
  ];

  static const restrictedValues = [
    SourceSite('huangdou', '黄豆', '精选短剧'),
    SourceSite('huangju', '剧果', '热门 · 最新 · 分类短剧'),
    SourceSite('yeguo', '野果', '分类短剧 · 在线搜索'),
    dsd,
    jinguoduanju,
    jinguo,
    heitao,
    SourceSite('xchina', 'XChina', '分类视频 · 在线搜索'),
    SourceSite('theporny', 'Theporny', '分类视频 · 在线搜索'),
    SourceSite('huangguo-video', '黄果视频', '视频剧集'),
    SourceSite('huangguoai', '黄果 AI', 'AI 短剧'),
    SourceSite('cloudfront', '黄果旧版', '旧 API 剧库'),
    ...videoValues,
    // XChina 实时上新：固定按上传时间倒序，刷新整体替换第一页，用来验证上新。
    xchinaLive,
    p91,
    beabox,
    kissav,
    viral,
  ];
  static const knownValues = [...primaryValues, ...restrictedValues];
  static const allValues = [...primaryValues, ...restrictedValues];
  static const values = allSourcesEnabled ? knownValues : [fanqie, hongguo];

  /// 当前会话已注册的自定义源 ID，供可用性判断与站源归一化使用。
  static final Set<String> customIds = {};

  /// 自定义源 ID → 显示名，让观看记录、追剧、下载等处的来源标签能显示真实名称。
  static final Map<String, String> customNames = {};
  static bool isCustomId(String id) => id.startsWith('custom:');
  static bool isAvailable(String id) =>
      values.any((site) => site.id == id) || customIds.contains(id);
  static bool isKnown(String id) =>
      allValues.any((site) => site.id == id) || customIds.contains(id);
  static bool isRestricted(String id) =>
      restrictedValues.any((site) => site.id == id);
  static String customName(String id) {
    if (id.startsWith('custom:')) return customNames[id] ?? '自定义源';
    return id;
  }

  static SourceSite byId(String id) {
    if (customIds.contains(id)) {
      return SourceSite(id, customName(id), '自定义站源');
    }
    return allValues.firstWhere((site) => site.id == id, orElse: () => hongguo);
  }
}

class CustomSourceSite {
  const CustomSourceSite({
    required this.id,
    required this.name,
    required this.base,
    this.rule = '',
    this.kind = '',
  });
  final String id;
  final String name;
  final String base;
  final String rule;

  /// 源类型：web=网页源（走 HTML 抓取）/ json=JSON 源（直连 API）/ 空=自动识别。
  final String kind;

  factory CustomSourceSite.fromJson(Map<String, dynamic> json) =>
      CustomSourceSite(
        id: json['id'] as String? ?? '',
        name: json['name'] as String? ?? '',
        base: json['base'] as String? ?? '',
        rule: json['rule'] as String? ?? '',
        kind: (json['kind'] as String? ?? json['type'] as String? ?? ''),
      );
  Map<String, dynamic> toJson() => {
    'id': id,
    'name': name,
    'base': base,
    if (rule.isNotEmpty) 'rule': rule,
    if (kind.isNotEmpty) 'kind': kind,
  };
  bool get isCustom => SourceSite.isCustomId(id);
  bool get hasRule => rule.trim().isNotEmpty;
  SourceSite get asSourceSite => SourceSite(id, name, base);
}

class SourceGroup {
  const SourceGroup(this.id, this.name, this.sources);
  final String id;
  final String name;
  final List<SourceSite> sources;

  static List<SourceGroup> fromSources(Iterable<SourceSite> sources) {
    final groups = <String, List<SourceSite>>{};
    for (final source in sources) {
      (groups[source.groupId] ??= []).add(source);
    }
    return [
      for (final group in groups.entries)
        SourceGroup(group.key, group.value.first.groupName, group.value),
    ];
  }
}

class CatalogCategory {
  const CatalogCategory(this.id, this.name, {this.local = false});
  static const all = CatalogCategory('', '全部');
  final String id;
  final String name;
  final bool local;
  factory CatalogCategory.fromJson(Map<String, dynamic> json) =>
      CatalogCategory(
        json['id'] as String? ?? '',
        json['name'] as String? ?? '全部',
      );
}

int intValue(Object? value) =>
    value is num ? value.toInt() : int.tryParse('$value') ?? 0;

class Drama {
  const Drama({
    required this.id,
    required this.source,
    required this.title,
    this.sourceId = '',
    this.description = '',
    this.cover = '',
    this.episodes = 0,
    this.category = '',
    bool? vip,
    this.heat = '',
    this.views = '',
    this.onlineDate = '',
    this.tags = const [],
    this.releaseStatus = '',
    this.author = '',
    this.authorId = '',
    this.peers = const [],
  }) : vipStatus = vip;
  final String id;
  final String source;
  final String sourceId;
  final String title;
  final String description;
  final String cover;
  final int episodes;
  final String category;
  final bool? vipStatus;
  bool get vip => vipStatus == true;
  final String heat;
  final String views;
  final String onlineDate;
  final List<String> tags;
  final String releaseStatus;

  /// 发布者（91porn 的上传者）。authorId 非空时详情页可以把作者名做成可点击，
  /// 进作者主页看他发布的其它作品。
  final String author;
  final String authorId;

  /// 跨源聚合搜索里同一部剧在其它站源的副本，供「全部源」结果展示与切换播放源。
  final List<Drama> peers;
  String get releaseLabel => switch (releaseStatus) {
    'finished' || 'completed' => '已完结',
    'ongoing' => '连载中',
    _ => '状态未知',
  };

  factory Drama.fromJson(Map<String, dynamic> json) => Drama(
    id: json['id'] as String? ?? '',
    source: json['source'] as String? ?? 'hongguo',
    sourceId: json['sourceId'] as String? ?? '',
    title: json['title'] as String? ?? '短剧',
    description: json['description'] as String? ?? '',
    cover: json['cover'] as String? ?? '',
    episodes: intValue(json['episodes']),
    category: json['category'] as String? ?? '',
    vip: json['vip'] == true
        ? true
        : json['vip'] == false &&
              (json['source'] != 'huangdou' ||
                  intValue(json['metadataSchema']) >= 1)
        ? false
        : null,
    heat: json['heat']?.toString() ?? '',
    views: json['views']?.toString() ?? '',
    onlineDate: json['onlineDate'] as String? ?? '',
    tags: (json['tags'] as List? ?? const []).whereType<String>().toList(),
    releaseStatus: json['releaseStatus'] as String? ?? '',
    author: json['author'] as String? ?? '',
    authorId: json['authorId'] as String? ?? '',
    peers: [
      for (final row in json['peers'] as List? ?? const [])
        Drama.fromJson(Map<String, dynamic>.from(row as Map)),
    ],
  );
  Map<String, dynamic> toJson() => {
    'metadataSchema': 1,
    'id': id,
    'source': source,
    'sourceId': sourceId,
    'title': title,
    'description': description,
    'cover': cover,
    'episodes': episodes,
    'category': category,
    'vip': vipStatus,
    'heat': heat,
    'views': views,
    'onlineDate': onlineDate,
    'tags': tags,
    'releaseStatus': releaseStatus,
    if (author.isNotEmpty) 'author': author,
    if (authorId.isNotEmpty) 'authorId': authorId,
  };

  Drama merge(Drama fresh) {
    if (id != fresh.id) return fresh;
    const genericCategories = {
      '短剧',
      '真人剧',
      '漫剧',
      'AI剧',
      'AI 剧',
      'AI短剧',
      'AI 短剧',
      'AI漫剧',
      'AI 漫剧',
    };
    return Drama(
      id: id,
      source: fresh.source.isEmpty ? source : fresh.source,
      sourceId: fresh.sourceId.isEmpty ? sourceId : fresh.sourceId,
      title: fresh.title.isEmpty || fresh.title == '短剧' ? title : fresh.title,
      description: fresh.description.isEmpty ? description : fresh.description,
      cover: fresh.cover.isEmpty ? cover : fresh.cover,
      episodes: fresh.episodes > 0 ? fresh.episodes : episodes,
      category:
          fresh.category.isEmpty ||
              genericCategories.contains(fresh.category) &&
                  category.isNotEmpty &&
                  !genericCategories.contains(category)
          ? category
          : fresh.category,
      vip: fresh.vipStatus ?? vipStatus,
      heat: fresh.heat.isEmpty ? heat : fresh.heat,
      views: fresh.views.isEmpty ? views : fresh.views,
      onlineDate: fresh.onlineDate.isEmpty ? onlineDate : fresh.onlineDate,
      tags: fresh.tags.isEmpty ? tags : fresh.tags,
      releaseStatus:
          fresh.releaseStatus.isEmpty || fresh.releaseStatus == 'unknown'
          ? releaseStatus
          : fresh.releaseStatus,
      author: fresh.author.isEmpty ? author : fresh.author,
      authorId: fresh.authorId.isEmpty ? authorId : fresh.authorId,
    );
  }
}

class Episode {
  Episode(this.raw, int fallback)
    : id = raw['id'] as String? ?? '',
      title = raw['title'] as String? ?? '第$fallback集',
      number = intValue(raw['currentEpisode']) > 0
          ? intValue(raw['currentEpisode'])
          : fallback,
      vip = raw['vip'] == true,
      vertical = raw['vertical'] as bool?;
  final Map<String, dynamic> raw;
  final String id;
  final String title;
  final int number;
  final bool vip;
  final bool? vertical;
}

class DramaDetail {
  DramaDetail(this.drama, this.episodes, {this.warning = ''});
  final Drama drama;
  final List<Episode> episodes;
  final String warning;
  factory DramaDetail.fromJson(Map<String, dynamic> json) {
    final rows = json['chapters'] as List? ?? const [];
    return DramaDetail(
      Drama.fromJson(Map<String, dynamic>.from(json['drama'] as Map)),
      [
        for (var i = 0; i < rows.length; i++)
          Episode(Map<String, dynamic>.from(rows[i] as Map), i + 1),
      ],
      warning: json['warning'] as String? ?? '',
    );
  }
}

class CatalogPage {
  CatalogPage(
    this.items, {
    this.hasMore = false,
    this.warning = '',
    this.page = 1,
    this.fresh = false,
  });
  final List<Drama> items;
  final bool hasMore;
  final String warning;
  final int page;
  final bool fresh;
  factory CatalogPage.fromJson(Map<String, dynamic> json) => CatalogPage(
    [
      for (final row in json['items'] as List? ?? const [])
        Drama.fromJson(Map<String, dynamic>.from(row as Map)),
    ],
    hasMore: json['hasMore'] == true,
    warning: json['warning'] as String? ?? '',
    page: intValue(json['page']) > 0 ? intValue(json['page']) : 1,
    fresh: json['fresh'] == true,
  );
}

/// 跨源聚合搜索的进度快照。
///
/// 原生核心一边搜一边回：每条 [items] 是「截至目前所有已返回站源」的归一去重结果，
/// [done] / [total] 表示完成进度，[pending] 是还没回来的站源。界面据此边搜边显示，
/// 不必等最慢的站源。
class SearchProgress {
  const SearchProgress({
    this.id = '',
    this.query = '',
    this.items = const [],
    this.sources = const [],
    this.pending = const [],
    this.failures = const {},
    this.warning = '',
    this.total = 0,
    this.done = 0,
    this.finished = false,
  });
  final String id;
  final String query;
  final List<Drama> items;

  /// 已成功返回的站源。
  final List<String> sources;

  /// 仍在搜索中的站源。
  final List<String> pending;
  final Map<String, String> failures;
  final String warning;
  final int total;
  final int done;
  final bool finished;

  bool get running => !finished;

  factory SearchProgress.fromJson(Map<String, dynamic> json) => SearchProgress(
    id: json['search'] as String? ?? '',
    query: json['query'] as String? ?? '',
    items: [
      for (final row in json['items'] as List? ?? const [])
        Drama.fromJson(Map<String, dynamic>.from(row as Map)),
    ],
    sources: (json['sources'] as List? ?? const [])
        .whereType<String>()
        .toList(),
    pending: (json['pending'] as List? ?? const [])
        .whereType<String>()
        .toList(),
    failures: {
      for (final entry in (json['failures'] as Map? ?? const {}).entries)
        entry.key as String: '${entry.value}',
    },
    warning: json['warning'] as String? ?? '',
    total: intValue(json['total']),
    done: intValue(json['done']),
    finished: json['finished'] == true,
  );
}

class PlaybackPlan {
  const PlaybackPlan({
    required this.url,
    this.directUrl = '',
    this.headers = const {},
    this.decryptionKey = '',
    this.quality = 0,
    this.qualities = const [],
    this.session = '',
    this.danmakuId = '',
    this.prefetchedBytes = 0,
    this.expiresAt = 0,
    this.routeIndex = 0,
    this.routeCount = 1,
    this.local = false,
  });
  final String url;

  /// 站源返回的原始播放地址，未经本机中转。投屏时优先用它，电视自己上网
  /// 拉流，手机退出 App 也能继续播。空串表示这条线路只能经手机转发
  /// （本地文件、需要本机解密或重建播放列表的加密线路）。
  final String directUrl;

  final Map<String, String> headers;
  final String decryptionKey;
  final int quality;
  final List<int> qualities;
  final String session;
  final String danmakuId;
  final int prefetchedBytes;
  final int expiresAt;
  final int routeIndex;
  final int routeCount;
  final bool local;
  bool get hasAlternative => session.isNotEmpty && routeIndex + 1 < routeCount;
  factory PlaybackPlan.fromJson(Map<String, dynamic> json) => PlaybackPlan(
    url: json['url'] as String? ?? '',
    directUrl: json['directUrl'] as String? ?? '',
    local: json['local'] == true,
    headers: (json['headers'] as Map? ?? {}).map(
      (key, value) => MapEntry(key.toString(), value.toString()),
    ),
    decryptionKey: json['decryptionKey'] as String? ?? '',
    quality: intValue(json['quality']),
    qualities: (json['qualities'] as List? ?? []).map(intValue).toSet().toList()
      ..sort((a, b) => b.compareTo(a)),
    session: json['session'] as String? ?? '',
    danmakuId: json['danmakuId'] as String? ?? '',
    prefetchedBytes: intValue(json['prefetchedBytes']),
    expiresAt: intValue(json['expiresAt']),
    routeIndex: intValue(json['routeIndex']),
    routeCount: intValue(json['routeCount']) > 0
        ? intValue(json['routeCount'])
        : 1,
  );
}

class WatchEntry {
  WatchEntry({
    required this.drama,
    required this.episode,
    required this.position,
    required this.duration,
    required this.updatedAt,
  });
  final Drama drama;
  final int episode;
  final double position;
  final double duration;
  final DateTime updatedAt;
  bool get finished => duration > 1 && position >= duration - 1;
  Map<String, dynamic> toJson() => {
    'drama': drama.toJson(),
    'episode': episode,
    'position': position,
    'duration': duration,
    'updatedAt': updatedAt.toIso8601String(),
  };
  factory WatchEntry.fromJson(Map<String, dynamic> json) => WatchEntry(
    drama: Drama.fromJson(Map<String, dynamic>.from(json['drama'] as Map)),
    episode: intValue(json['episode']),
    position: (json['position'] as num?)?.toDouble() ?? 0,
    duration: (json['duration'] as num?)?.toDouble() ?? 0,
    updatedAt:
        DateTime.tryParse(json['updatedAt'].toString()) ?? DateTime(2000),
  );
}

class DownloadJob {
  const DownloadJob({
    required this.id,
    required this.drama,
    required this.episode,
    required this.state,
    this.bytes = 0,
    this.total = 0,
    this.progress = 0,
    this.quality = 0,
    this.actualQuality = 0,
    this.error = '',
    this.created = 0,
    this.revision = 0,
    this.archived = false,
  });
  final int created;
  final int revision;
  final bool archived;
  final String id;
  final Drama drama;
  final Episode episode;
  final String state;
  final int bytes;
  final int total;
  final double progress;
  final int quality;
  final int actualQuality;
  final String error;
  bool get completed => state == 'completed';
  bool get active => state == 'queued' || state == 'downloading';
  bool get resumable => state == 'paused' || state == 'failed';
  bool get converting => state == 'converting';
  String get stateLabel => switch (state) {
    'queued' => '等待下载',
    'downloading' => '正在下载',
    'converting' => '正在处理',
    'paused' => '已暂停',
    'failed' => '下载失败',
    'completed' => '已下载',
    'removing' => '正在取消',
    _ => '等待更新',
  };
  factory DownloadJob.fromJson(Map<String, dynamic> value) => DownloadJob(
    id: value['id'] as String? ?? '',
    drama: Drama.fromJson(Map<String, dynamic>.from(value['drama'] as Map)),
    episode: Episode(
      Map<String, dynamic>.from(value['chapter'] as Map),
      intValue(value['index']),
    ),
    state: value['state'] as String? ?? 'failed',
    bytes: intValue(value['bytes']),
    total: intValue(value['total']),
    progress: ((value['progress'] as num?)?.toDouble() ?? 0).clamp(0, 1),
    quality: intValue(value['quality']),
    actualQuality: intValue(value['actualQuality']),
    error: value['error'] as String? ?? '',
    created: intValue(value['created']),
    revision: intValue(value['revision']),
    archived: value['archived'] == true,
  );
}

typedef DownloadConversion = ({String input, String key, String output});

List<Map<String, dynamic>> readJsonList(String? value) {
  try {
    return (jsonDecode(value ?? '[]') as List)
        .whereType<Map>()
        .map((row) => Map<String, dynamic>.from(row))
        .toList();
  } catch (_) {
    return [];
  }
}
