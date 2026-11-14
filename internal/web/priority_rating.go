package web

import (
	"fmt"
	"math"

	"werkstatt-hub/internal/core"
)

type wrenchStep struct {
	Index  int
	Active bool
}

type priorityRating struct {
	Label       string
	Description string
	ValueText   string
	Unset       bool
	Items       []wrenchStep
}

// priorityRenderer keeps the stored priority intact while showing its relative
// position on five wrench icons, including installations with a custom scale.
func priorityRenderer(scale core.PriorityScale) func(*int, string) priorityRating {
	if !scale.Valid() {
		scale = core.DefaultPriorityScale()
	}
	return func(value *int, label string) priorityRating {
		rating := priorityRating{Label: label, Unset: value == nil, Items: make([]wrenchStep, 5)}
		active := 0
		if value == nil {
			rating.Description = label + ": Noch nicht gesetzt"
			rating.ValueText = "Noch nicht gesetzt"
		} else {
			position := max(scale.Min, min(scale.Max, *value))
			active = 1 + int(math.Round(4*float64(position-scale.Min)/float64(scale.Max-scale.Min)))
			rating.Description = fmt.Sprintf("%s: %d (Skala %d bis %d), %d von 5 Schraubenschlüsseln", label, *value, scale.Min, scale.Max, active)
			if scale.Min == 1 && scale.Max == 5 {
				rating.ValueText = fmt.Sprintf("%d/5", *value)
			} else {
				rating.ValueText = fmt.Sprintf("%d · Skala %d–%d", *value, scale.Min, scale.Max)
			}
		}
		for i := range rating.Items {
			rating.Items[i] = wrenchStep{Index: i + 1, Active: i < active}
		}
		return rating
	}
}
