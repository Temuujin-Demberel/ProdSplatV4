package jobs

import "errors"

type ReconstructionProfile struct {
	Name               string `json:"name"`
	Frames             int    `json:"frames"`
	Iterations         int    `json:"iterations"`
	DataDownscale      int    `json:"dataDownscale"`
	ModelDownscales    int    `json:"modelDownscales"`
	ResolutionSchedule int    `json:"resolutionSchedule"`
	Description        string `json:"description"`
}

var Profiles = map[string]ReconstructionProfile{
	"fast": {
		Name: "fast", Frames: 60, Iterations: 4000, DataDownscale: 2, ModelDownscales: 2, ResolutionSchedule: 3000,
		Description: "Fast preset. Frames is a minimum; long videos are sampled adaptively.",
	},
	"balanced": {
		Name: "balanced", Frames: 90, Iterations: 8000, DataDownscale: 2, ModelDownscales: 1, ResolutionSchedule: 2000,
		Description: "Default preset. Keeps the validated 90-frame baseline for short captures and increases frames adaptively for long videos.",
	},
	"quality": {
		Name: "quality", Frames: 140, Iterations: 15000, DataDownscale: 1, ModelDownscales: 1, ResolutionSchedule: 2000,
		Description: "Higher-quality preset with adaptive frame sampling and substantially greater GPU memory/time requirements.",
	},
}

func Profile(name string) (ReconstructionProfile, error) {
	p, ok := Profiles[name]
	if !ok {
		return ReconstructionProfile{}, errors.New("unknown reconstruction profile")
	}
	return p, nil
}
