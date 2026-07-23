export type TimeZoneOption = {
  label: string
  value: string
}

export type TimeZoneOptionGroup = {
  label: string
  options: TimeZoneOption[]
}

export const timeZoneOptionGroups: TimeZoneOptionGroup[] = [
  {
    label: '协调世界时',
    options: [
      { label: '协调世界时 (UTC)', value: 'UTC' },
    ],
  },
  {
    label: '东亚、东南亚与南亚',
    options: [
      { label: '中国标准时间 (UTC+08:00)', value: 'Asia/Shanghai' },
      { label: '香港时间', value: 'Asia/Hong_Kong' },
      { label: '台北时间', value: 'Asia/Taipei' },
      { label: '日本标准时间 (UTC+09:00)', value: 'Asia/Tokyo' },
      { label: '韩国标准时间 (首尔)', value: 'Asia/Seoul' },
      { label: '乌兰巴托时间', value: 'Asia/Ulaanbaatar' },
      { label: '新加坡时间', value: 'Asia/Singapore' },
      { label: '吉隆坡时间', value: 'Asia/Kuala_Lumpur' },
      { label: '曼谷时间', value: 'Asia/Bangkok' },
      { label: '雅加达时间 (UTC+07:00)', value: 'Asia/Jakarta' },
      { label: '望加锡时间 (UTC+08:00)', value: 'Asia/Makassar' },
      { label: '查亚普拉时间 (UTC+09:00)', value: 'Asia/Jayapura' },
      { label: '马尼拉时间', value: 'Asia/Manila' },
      { label: '胡志明市时间', value: 'Asia/Ho_Chi_Minh' },
      { label: '金边时间', value: 'Asia/Phnom_Penh' },
      { label: '仰光时间 (UTC+06:30)', value: 'Asia/Yangon' },
      { label: '达卡时间', value: 'Asia/Dhaka' },
      { label: '印度标准时间 (加尔各答，UTC+05:30)', value: 'Asia/Kolkata' },
      { label: '加德满都时间 (UTC+05:45)', value: 'Asia/Kathmandu' },
      { label: '科伦坡时间', value: 'Asia/Colombo' },
      { label: '卡拉奇时间', value: 'Asia/Karachi' },
    ],
  },
  {
    label: '中东',
    options: [
      { label: '迪拜时间', value: 'Asia/Dubai' },
      { label: '马斯喀特时间', value: 'Asia/Muscat' },
      { label: '德黑兰时间', value: 'Asia/Tehran' },
      { label: '巴格达时间', value: 'Asia/Baghdad' },
      { label: '利雅得时间', value: 'Asia/Riyadh' },
      { label: '耶路撒冷时间', value: 'Asia/Jerusalem' },
      { label: '伊斯坦布尔时间', value: 'Europe/Istanbul' },
    ],
  },
  {
    label: '欧洲',
    options: [
      { label: '莫斯科时间', value: 'Europe/Moscow' },
      { label: '赫尔辛基时间', value: 'Europe/Helsinki' },
      { label: '雅典时间', value: 'Europe/Athens' },
      { label: '布加勒斯特时间', value: 'Europe/Bucharest' },
      { label: '欧洲中部时间 (柏林)', value: 'Europe/Berlin' },
      { label: '巴黎时间', value: 'Europe/Paris' },
      { label: '罗马时间', value: 'Europe/Rome' },
      { label: '马德里时间', value: 'Europe/Madrid' },
      { label: '阿姆斯特丹时间', value: 'Europe/Amsterdam' },
      { label: '布鲁塞尔时间', value: 'Europe/Brussels' },
      { label: '华沙时间', value: 'Europe/Warsaw' },
      { label: '布拉格时间', value: 'Europe/Prague' },
      { label: '维也纳时间', value: 'Europe/Vienna' },
      { label: '苏黎世时间', value: 'Europe/Zurich' },
      { label: '斯德哥尔摩时间', value: 'Europe/Stockholm' },
      { label: '奥斯陆时间', value: 'Europe/Oslo' },
      { label: '哥本哈根时间', value: 'Europe/Copenhagen' },
      { label: '英国时间 (伦敦)', value: 'Europe/London' },
      { label: '都柏林时间', value: 'Europe/Dublin' },
      { label: '里斯本时间', value: 'Europe/Lisbon' },
      { label: '雷克雅未克时间', value: 'Atlantic/Reykjavik' },
    ],
  },
  {
    label: '非洲',
    options: [
      { label: '开罗时间', value: 'Africa/Cairo' },
      { label: '约翰内斯堡时间', value: 'Africa/Johannesburg' },
      { label: '内罗毕时间', value: 'Africa/Nairobi' },
      { label: '拉各斯时间', value: 'Africa/Lagos' },
      { label: '卡萨布兰卡时间', value: 'Africa/Casablanca' },
      { label: '阿尔及尔时间', value: 'Africa/Algiers' },
    ],
  },
  {
    label: '美洲',
    options: [
      { label: '纽芬兰时间 (圣约翰)', value: 'America/St_Johns' },
      { label: '大西洋时间 (哈利法克斯)', value: 'America/Halifax' },
      { label: '多伦多时间', value: 'America/Toronto' },
      { label: '美国东部时间 (纽约)', value: 'America/New_York' },
      { label: '美国中部时间 (芝加哥)', value: 'America/Chicago' },
      { label: '温尼伯时间', value: 'America/Winnipeg' },
      { label: '美国山地时间 (丹佛)', value: 'America/Denver' },
      { label: '凤凰城时间', value: 'America/Phoenix' },
      { label: '美国西部时间 (洛杉矶)', value: 'America/Los_Angeles' },
      { label: '温哥华时间', value: 'America/Vancouver' },
      { label: '阿拉斯加时间 (安克雷奇)', value: 'America/Anchorage' },
      { label: '夏威夷时间 (檀香山)', value: 'Pacific/Honolulu' },
      { label: '蒂华纳时间', value: 'America/Tijuana' },
      { label: '墨西哥城时间', value: 'America/Mexico_City' },
      { label: '波哥大时间', value: 'America/Bogota' },
      { label: '利马时间', value: 'America/Lima' },
      { label: '圣地亚哥时间', value: 'America/Santiago' },
      { label: '圣保罗时间', value: 'America/Sao_Paulo' },
      { label: '布宜诺斯艾利斯时间', value: 'America/Argentina/Buenos_Aires' },
      { label: '蒙得维的亚时间', value: 'America/Montevideo' },
    ],
  },
  {
    label: '大洋洲',
    options: [
      { label: '珀斯时间', value: 'Australia/Perth' },
      { label: '达尔文时间 (UTC+09:30)', value: 'Australia/Darwin' },
      { label: '阿德莱德时间', value: 'Australia/Adelaide' },
      { label: '布里斯班时间', value: 'Australia/Brisbane' },
      { label: '悉尼时间', value: 'Australia/Sydney' },
      { label: '墨尔本时间', value: 'Australia/Melbourne' },
      { label: '霍巴特时间', value: 'Australia/Hobart' },
      { label: '奥克兰时间', value: 'Pacific/Auckland' },
      { label: '斐济时间', value: 'Pacific/Fiji' },
      { label: '关岛时间', value: 'Pacific/Guam' },
      { label: '莫尔兹比港时间', value: 'Pacific/Port_Moresby' },
    ],
  },
]
