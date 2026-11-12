package biz

import (
	"strings"
	"testing"
)

func TestParseSignalAndSubSKUs(t *testing.T) {
	text := "变体 30-1 与 30-2 已整理。\n" + SignalFence(map[string]any{
		"signal":   "text_draft",
		"sub_skus": []map[string]string{{"offer_id": "30-1", "specs": "深空灰"}},
		"title_ru": "Держатель 30",
	})
	sig, err := ParseSignal(text)
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil || sig.Signal != "text_draft" || sig.TitleRU != "Держатель 30" {
		t.Fatalf("signal = %#v", sig)
	}
	skus := MergeSubSKUs("30", sig.SubSKUs, text)
	if len(skus) != 2 || skus[0].OfferID != "30-1" || skus[1].OfferID != "30-2" {
		t.Fatalf("sub skus = %#v", skus)
	}
	if skus[0].Specs != "深空灰" {
		t.Fatalf("specs lost: %#v", skus[0])
	}
}

func TestParseSignalIgnoresOrdinaryProse(t *testing.T) {
	sig, err := ParseSignal("当前主 SKU 为 30，审核状态未变。")
	if err != nil {
		t.Fatal(err)
	}
	if sig != nil {
		t.Fatalf("expected no signal, got %#v", sig)
	}
}

func TestParseSignalRejectsBrokenFence(t *testing.T) {
	_, err := ParseSignal("```ozon-signal\n{not json}\n```")
	if err == nil {
		t.Fatal("expected invalid json to fail")
	}
}

func TestMergeSubSKUsDoesNotSplitLongerSuffix(t *testing.T) {
	skus := MergeSubSKUs("30", nil, "货号 30-10 和前缀 130-1 都不是短货号")
	if len(skus) != 1 || skus[0].OfferID != "30-10" {
		t.Fatalf("skus = %#v", skus)
	}
}

func TestRedactSecrets(t *testing.T) {
	in := "请求头 Api-Key: supersecret\nAuthorization: Bearer abc.def\n{\"api_key\":\"abc123\",\"client_id\":\"shop\"}\n主 SKU 30-1"
	got := Redact(in)
	for _, secret := range []string{"supersecret", "abc.def", "abc123"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret %s survived: %s", secret, got)
		}
	}
	if !strings.Contains(got, "Api-Key: ***") || !strings.Contains(got, "主 SKU 30-1") {
		t.Fatalf("redacted = %s", got)
	}
}
