package jobs

import "testing"

func TestSanitizeAssetName(t *testing.T) {
	cases := map[string]string{
		"Orgiluun Tropical Can":   "Orgiluun_Tropical_Can",
		"orgiluun_lemon_lime_Pet": "orgiluun_lemon_lime_Pet",
		"  sengur--can  ":         "sengur_can",
		"a__b":                    "a_b",
		"_x_":                     "x",
		"Sengur Can (0.5L)":       "Sengur_Can_0_5L",
		"---":                     "",
		"Сэнгүр can":              "can",
	}
	for raw, want := range cases {
		if got := SanitizeAssetName(raw); got != want {
			t.Errorf("SanitizeAssetName(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestResolveRenderOptions(t *testing.T) {
	got, err := ResolveRenderOptions("Bottle Test", RenderOptions{})
	if err != nil || got.AssetName != "Bottle_Test" || got.UpAxis != DefaultUpAxis || got.FrontAzimuthDegrees != 0 {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	got, err = ResolveRenderOptions("Bottle", RenderOptions{AssetName: "a__b", UpAxis: "-y", FrontAzimuthDegrees: 337.5})
	if err != nil || got.AssetName != "a_b" || got.UpAxis != "-y" || got.FrontAzimuthDegrees != 337.5 {
		t.Fatalf("explicit: %+v %v", got, err)
	}
	invalid := []RenderOptions{
		{AssetName: "---"},
		{UpAxis: "+w"},
		{FrontAzimuthDegrees: 10},
		{FrontAzimuthDegrees: -22.5},
		{FrontAzimuthDegrees: 360},
	}
	for _, bad := range invalid {
		if _, err := ResolveRenderOptions("Bottle", bad); err == nil {
			t.Errorf("expected error for %+v", bad)
		}
	}
	if _, err := ResolveRenderOptions("---", RenderOptions{}); err == nil {
		t.Error("expected error when the job name has no letters or digits")
	}
}
