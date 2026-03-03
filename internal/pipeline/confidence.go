package pipeline

import "github.com/tgeorge06/atlaskb/internal/models"

// RelConfidence computes a relationship confidence score by applying a strength
// delta to a base tier value. The result is clamped to [0.0, 1.0].
func RelConfidence(baseTier float32, strength string) float32 {
	return models.ClampConfidence(baseTier + models.StrengthToConfidenceDelta(strength))
}
