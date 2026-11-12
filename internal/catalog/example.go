// Package catalog holds the offline category sample and the shapes shared with the Ozon client.
package catalog

// TreeNode is one element of POST /v1/description-category/tree result.
type TreeNode struct {
	DescriptionCategoryID int64      `json:"description_category_id,omitempty"`
	CategoryName          string     `json:"category_name,omitempty"`
	TypeID                int64      `json:"type_id,omitempty"`
	TypeName              string     `json:"type_name,omitempty"`
	Disabled              bool       `json:"disabled"`
	Children              []TreeNode `json:"children"`
}

type labels struct {
	auto, parts, chargerCat, mount, charger, disabledCat, disabledType string
	tools, jack, apparel, hats, knit, cap, home, kitchen, glass        string
}

func names(language string) labels {
	switch language {
	case "RU", "DEFAULT":
		return labels{
			auto: "Автотовары", parts: "Автоаксессуары", chargerCat: "Автоаксессуары",
			mount: "Держатель для телефона", charger: "Автомобильное зарядное устройство",
			disabledCat: "Архивные аксессуары", disabledType: "Снятый с продажи тип",
			tools: "Инструменты", jack: "Домкрат гидравлический",
			apparel: "Аксессуары", hats: "Головные уборы", knit: "Шапка вязаная", cap: "Бейсболка",
			home: "Дом и сад", kitchen: "Посуда", glass: "Стакан",
		}
	case "EN":
		return labels{
			auto: "Auto goods", parts: "In-car accessories", chargerCat: "In-car accessories",
			mount: "Phone holder", charger: "Car charger",
			disabledCat: "Retired accessories", disabledType: "Discontinued type",
			tools: "Repair tools", jack: "Hydraulic jack",
			apparel: "Apparel", hats: "Hats", knit: "Knit hat", cap: "Baseball cap",
			home: "Home", kitchen: "Tableware", glass: "Drinking glass",
		}
	case "TR":
		return labels{
			auto: "Oto", parts: "Araç içi", chargerCat: "Araç içi",
			mount: "Telefon tutucu", charger: "Araç şarj cihazı",
			disabledCat: "Kullanımdan kalkan", disabledType: "Satışta değil",
			tools: "Aletler", jack: "Hidrolik kriko",
			apparel: "Giyim", hats: "Şapkalar", knit: "Örme bere", cap: "Beyzbol şapkası",
			home: "Ev", kitchen: "Mutfak", glass: "Bardak",
		}
	default:
		return labels{
			auto: "汽车用品", parts: "车载配件", chargerCat: "车载配件",
			mount: "车载手机支架", charger: "车载充电器",
			disabledCat: "停用配件", disabledType: "已停售类型",
			tools: "维修工具", jack: "汽车千斤顶",
			apparel: "服装配饰", hats: "帽子和头饰", knit: "针织帽", cap: "棒球帽",
			home: "家居用品", kitchen: "餐厨器具", glass: "玻璃杯",
		}
	}
}

// IDs used by the offline sample and the in-process Ozon mock.
const (
	CatAuto     int64 = 100
	CatParts    int64 = 17028922
	CatRetired  int64 = 17029999
	CatTools    int64 = 17120001
	CatApparel  int64 = 200
	CatHats     int64 = 17021004
	CatHome     int64 = 300
	CatKitchen  int64 = 17021555
	TypeMount   int64 = 97062518
	TypeCharger int64 = 97062519
	TypeRetired int64 = 97000001
	TypeJack    int64 = 97120003
	TypeKnit    int64 = 97100345
	TypeCap     int64 = 97100346
	TypeGlass   int64 = 97180312
)

// ExampleTree is the offline sample. Category ids and type ids match the clickable prototype.
func ExampleTree(language string) []TreeNode {
	n := names(language)
	return []TreeNode{
		{
			DescriptionCategoryID: CatAuto,
			CategoryName:          n.auto,
			Children: []TreeNode{
				{
					DescriptionCategoryID: CatParts,
					CategoryName:          n.parts,
					Children: []TreeNode{
						{TypeName: n.mount, TypeID: TypeMount},
						{TypeName: n.charger, TypeID: TypeCharger},
					},
				},
				{
					DescriptionCategoryID: CatRetired,
					CategoryName:          n.disabledCat,
					Disabled:              true,
					Children: []TreeNode{
						{TypeName: n.disabledType, TypeID: TypeRetired},
					},
				},
				{
					DescriptionCategoryID: CatTools,
					CategoryName:          n.tools,
					Children: []TreeNode{
						{TypeName: n.jack, TypeID: TypeJack},
					},
				},
			},
		},
		{
			DescriptionCategoryID: CatApparel,
			CategoryName:          n.apparel,
			Children: []TreeNode{
				{
					DescriptionCategoryID: CatHats,
					CategoryName:          n.hats,
					Children: []TreeNode{
						{TypeName: n.knit, TypeID: TypeKnit},
						{TypeName: n.cap, TypeID: TypeCap, Disabled: true},
					},
				},
			},
		},
		{
			DescriptionCategoryID: CatHome,
			CategoryName:          n.home,
			Children: []TreeNode{
				{
					DescriptionCategoryID: CatKitchen,
					CategoryName:          n.kitchen,
					Children: []TreeNode{
						{TypeName: n.glass, TypeID: TypeGlass},
					},
				},
			},
		},
	}
}

func attrText(language, zh, ru, en string) string {
	switch language {
	case "RU", "DEFAULT":
		return ru
	case "EN", "TR":
		return en
	default:
		return zh
	}
}

func attribute(language string, id int64, zh, ru, en, typ string, required bool, groupZH, groupRU, groupEN string, groupID int64, extra map[string]any) map[string]any {
	m := map[string]any{
		"id":                    id,
		"attribute_complex_id":  int64(0),
		"name":                  attrText(language, zh, ru, en),
		"description":           attrText(language, zh+"。按 Ozon 属性契约填写。", "Заполните по контракту атрибута.", "Fill in according to the attribute contract."),
		"type":                  typ,
		"is_collection":         false,
		"is_required":           required,
		"is_aspect":             false,
		"max_value_count":       int64(0),
		"group_name":            attrText(language, groupZH, groupRU, groupEN),
		"group_id":              groupID,
		"dictionary_id":         int64(0),
		"category_dependent":    false,
		"complex_is_collection": false,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// ExampleAttributes returns the full required and optional contract for a sample type.
// Sibling types under the same category intentionally differ.
func ExampleAttributes(typeID int64, language string) []map[string]any {
	base := []map[string]any{
		attribute(language, 10096, "型号名称", "Название модели", "Model name", "String", true, "基本信息", "Основное", "General", 1, map[string]any{
			"description": attrText(language, "商品型号或系列名。", "Название модели.", "Model or series name."),
		}),
		attribute(language, 85, "品牌", "Бренд", "Brand", "String", true, "基本信息", "Основное", "General", 1, map[string]any{
			"dictionary_id":      int64(85),
			"category_dependent": true,
			"description":        attrText(language, "从品牌字典选择。无品牌时选「无品牌」。", "Выберите бренд из справочника.", "Pick a brand from the dictionary."),
		}),
		attribute(language, 10097, "颜色名称", "Цвет", "Color", "String", false, "规格", "Характеристики", "Specs", 2, map[string]any{
			"is_aspect":   true,
			"description": attrText(language, "变体颜色，可自由填写。", "Цвет варианта.", "Variant color, free text."),
		}),
		attribute(language, 10099, "主材质", "Материал", "Material", "String", false, "规格", "Характеристики", "Specs", 2, map[string]any{
			"dictionary_id": int64(503),
			"is_collection": true,
			"description":   attrText(language, "可多选的材质字典。", "Справочник материалов, несколько значений.", "Multi-value material dictionary."),
		}),
		attribute(language, 10100, "产地", "Страна производства", "Country of origin", "String", false, "基本信息", "Основное", "General", 1, map[string]any{
			"dictionary_id": int64(504),
			"description":   attrText(language, "产地字典。", "Страна производства.", "Country dictionary."),
		}),
		attribute(language, 10101, "包装件数", "Количество в упаковке", "Pieces per pack", "Integer", false, "包装", "Упаковка", "Package", 3, map[string]any{
			"description": attrText(language, "包装内件数。", "Число единиц в упаковке.", "Units inside the package."),
		}),
		attribute(language, 10102, "适用场景", "Сценарий использования", "Use case", "String", false, "规格", "Характеристики", "Specs", 2, map[string]any{
			"is_collection": true,
			"description":   attrText(language, "可填写多个场景。", "Можно указать несколько сценариев.", "Multiple use cases allowed."),
		}),
		attribute(language, 10103, "商品说明", "Описание", "Description", "String", false, "基本信息", "Основное", "General", 1, map[string]any{
			"description": attrText(language, "补充说明，选填。", "Дополнительное описание.", "Optional extra description."),
		}),
	}
	var specific map[string]any
	switch typeID {
	case TypeMount:
		specific = attribute(language, 10300, "安装方式", "Способ крепления", "Mounting method", "String", true, "规格", "Характеристики", "Specs", 2, map[string]any{
			"description": attrText(language, "例如出风口、吸盘或中控台。", "Например, дефлектор или присоска.", "For example vent, suction cup, or dashboard."),
		})
	case TypeCharger:
		specific = attribute(language, 10310, "输出功率", "Выходная мощность", "Output power", "Decimal", true, "规格", "Характеристики", "Specs", 2, map[string]any{
			"description": attrText(language, "额定输出功率，瓦特。", "Номинальная мощность, Вт.", "Rated output in watts."),
		})
		base = append(base, attribute(language, 10311, "接口类型", "Тип разъёма", "Connector", "String", false, "规格", "Характеристики", "Specs", 2, map[string]any{
			"dictionary_id": int64(510),
			"description":   attrText(language, "USB-A、USB-C 等字典值。", "USB-A, USB-C и другие значения справочника.", "USB-A, USB-C and other dictionary values."),
		}))
	case TypeJack:
		specific = attribute(language, 10320, "额定载重", "Грузоподъёмность", "Rated load", "Decimal", true, "规格", "Характеристики", "Specs", 2, map[string]any{
			"description": attrText(language, "额定举升重量，吨。", "Номинальная грузоподъёмность, т.", "Rated lifting capacity in tonnes."),
		})
	case TypeKnit:
		specific = attribute(language, 10330, "帽围", "Обхват головы", "Head circumference", "String", true, "规格", "Характеристики", "Specs", 2, map[string]any{
			"description": attrText(language, "适合的头围范围。", "Подходящий обхват головы.", "Suitable head circumference."),
		})
	case TypeCap:
		specific = attribute(language, 10340, "帽檐", "Козырёк", "Brim", "String", false, "规格", "Характеристики", "Specs", 2, map[string]any{
			"description": attrText(language, "帽檐款式，选填。该类型已停用，仅可查阅。", "Фасон козырька. Тип отключён.", "Brim style. This type is disabled."),
		})
	case TypeGlass:
		specific = attribute(language, 10350, "容量", "Объём", "Capacity", "Integer", true, "规格", "Характеристики", "Specs", 2, map[string]any{
			"description": attrText(language, "容量，毫升。", "Объём, мл.", "Capacity in millilitres."),
		})
	case TypeRetired:
		specific = attribute(language, 10360, "停用说明", "Примечание", "Retired note", "String", true, "基本信息", "Основное", "General", 1, map[string]any{
			"description": attrText(language, "父级类目已停用，类型仍可查阅。", "Родительская категория отключена.", "Parent category is disabled; the type stays readable."),
		})
	default:
		return []map[string]any{}
	}
	return append(base, specific)
}

// DictEntry is one value from attribute/values.
type DictEntry struct {
	ID      int64  `json:"id"`
	Value   string `json:"value"`
	Info    string `json:"info"`
	Picture string `json:"picture"`
}

// ExampleDictionary returns the sample dictionary for an attribute id. The second result is false when the attribute has no dictionary.
func ExampleDictionary(attributeID int64) ([]DictEntry, bool) {
	switch attributeID {
	case 85:
		return []DictEntry{
			{ID: 5055881, Value: "无品牌", Info: "没有品牌时使用"},
			{ID: 5056737, Value: "Bosch", Info: "博世"},
			{ID: 5057001, Value: "Xiaomi", Info: "小米"},
			{ID: 5057002, Value: "Baseus", Info: "倍思"},
			{ID: 5057003, Value: "70mai", Info: "70迈"},
			{ID: 5057004, Value: "HOCO", Info: "浩酷"},
			{ID: 5057005, Value: "Philips", Info: "飞利浦"},
			{ID: 5057006, Value: "Osram", Info: "欧司朗"},
		}, true
	case 503:
		return []DictEntry{
			{ID: 9001, Value: "ABS", Info: "塑料"},
			{ID: 9002, Value: "铝合金", Info: "金属"},
			{ID: 9003, Value: "硅胶", Info: "软胶"},
			{ID: 9004, Value: "腈纶", Info: "针织"},
			{ID: 9005, Value: "玻璃", Info: "钠钙玻璃"},
		}, true
	case 504:
		return []DictEntry{
			{ID: 9101, Value: "中国", Info: "CN"},
			{ID: 9102, Value: "俄罗斯", Info: "RU"},
			{ID: 9103, Value: "土耳其", Info: "TR"},
		}, true
	case 510:
		return []DictEntry{
			{ID: 9201, Value: "USB-A", Info: ""},
			{ID: 9202, Value: "USB-C", Info: ""},
			{ID: 9203, Value: "Lightning", Info: ""},
		}, true
	default:
		return nil, false
	}
}
