package codexcli

import "encoding/json"

// Reply 是每一轮 Codex 必须返回的业务信封。
type Reply struct {
	SchemaVersion string      `json:"schema_version"`
	Status        string      `json:"status"`
	Stage         string      `json:"stage"`
	Message       string      `json:"message"`
	Questions     []Question  `json:"questions"`
	Review        *Review     `json:"review"`
	Data          ReplyData   `json:"data"`
	Error         *ReplyError `json:"error"`
}

type Question struct {
	Field    string `json:"field"`
	Text     string `json:"text"`
	Required bool   `json:"required"`
}

type Review struct {
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
}

type ReplyData struct {
	Products   []Product   `json:"products"`
	VisualPlan []Visual    `json:"visual_plan"`
	Media      []MediaFile `json:"media"`
	Warnings   []string    `json:"warnings"`
}

type Product struct {
	SKURef            string     `json:"sku_ref"`
	OfferIDSuggestion *string    `json:"offer_id_suggestion"`
	VariantLabel      *string    `json:"variant_label"`
	Name              *string    `json:"name"`
	Cost              Cost       `json:"cost"`
	PrimaryImage      *string    `json:"primary_image"`
	Images            []string   `json:"images"`
	ColorImage        *string    `json:"color_image"`
	Dimensions        Dimensions `json:"dimensions"`
	Attributes        []Attr     `json:"attributes"`
	ComplexAttributes []Complex  `json:"complex_attributes"`
}

type Cost struct {
	PurchasePrice    *float64 `json:"purchase_price"`
	DomesticShipping *float64 `json:"domestic_shipping"`
	Total            *float64 `json:"total"`
	Currency         string   `json:"currency"`
	Evidence         *string  `json:"evidence"`
}

type Dimensions struct {
	Depth         *int   `json:"depth"`
	Width         *int   `json:"width"`
	Height        *int   `json:"height"`
	DimensionUnit string `json:"dimension_unit"`
	Weight        *int   `json:"weight"`
	WeightUnit    string `json:"weight_unit"`
}

type Attr struct {
	ID                int     `json:"id"`
	ComplexID         int     `json:"complex_id"`
	ValueText         *string `json:"value_text"`
	DictionaryValueID *int    `json:"dictionary_value_id"`
	Evidence          *string `json:"evidence"`
}

type Complex struct {
	ID        int            `json:"id"`
	ComplexID int            `json:"complex_id"`
	Values    []ComplexValue `json:"values"`
}

type ComplexValue struct {
	Value             *string `json:"value"`
	DictionaryValueID *int    `json:"dictionary_value_id"`
}

type Visual struct {
	Kind         string `json:"kind"`
	Count        int    `json:"count"`
	Instructions string `json:"instructions"`
}

type MediaFile struct {
	AssetID  string `json:"asset_id"`
	SKURef   string `json:"sku_ref"`
	Role     string `json:"role"`
	FilePath string `json:"file_path"`
	MimeType string `json:"mime_type"`
}

type ReplyError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func ParseReply(raw []byte) (Reply, error) {
	var reply Reply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return Reply{}, err
	}
	return reply, nil
}
