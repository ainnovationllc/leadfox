package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

//go:embed skills/leadfox-control-plan-review/SKILL.md
var controlPlanReviewSkill []byte

func controlPlanReviewSkillSHA256() string {
	digest := sha256.Sum256(controlPlanReviewSkill)
	return hex.EncodeToString(digest[:])
}
