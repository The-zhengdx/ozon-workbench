package biz

// SampleWarehouses are the in-process stand-ins returned only while OZON_MOCK is on.
// They are not warehouses of any seller account.
func SampleWarehouses() []Warehouse {
	return []Warehouse{
		{WarehouseID: 20605650762000, Name: "莫斯科一号仓", Status: "created", IsRFBS: false, WarehouseType: "FBS"},
		{WarehouseID: 20605650762001, Name: "圣彼得堡转运仓", Status: "created", IsRFBS: true, WarehouseType: "RFBS"},
		{WarehouseID: 20605650762002, Name: "喀山备货仓", Status: "created", IsRFBS: false, WarehouseType: "FBS"},
	}
}

// IsSampleWarehouse reports the built-in sample ids above.
func IsSampleWarehouse(id int64) bool {
	switch id {
	case 20605650762000, 20605650762001, 20605650762002:
		return true
	default:
		return false
	}
}

// StripUntrustedWarehouses removes built-in samples.
// flaggedMock means the whole list was produced by the mock client and must be dropped,
// even if a future sample id is added.
func StripUntrustedWarehouses(in []Warehouse, flaggedMock bool) (kept []Warehouse, changed bool) {
	if flaggedMock {
		if len(in) == 0 {
			return []Warehouse{}, false
		}
		return []Warehouse{}, true
	}
	kept = make([]Warehouse, 0, len(in))
	for _, item := range in {
		if IsSampleWarehouse(item.WarehouseID) {
			changed = true
			continue
		}
		kept = append(kept, item)
	}
	if !changed {
		if in == nil {
			return []Warehouse{}, false
		}
		return in, false
	}
	return kept, true
}
