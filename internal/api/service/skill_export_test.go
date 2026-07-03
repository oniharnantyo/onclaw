package service

import "github.com/oniharnantyo/onclaw/internal/store"

func MapSkillToView(sk *store.Skill) SkillView {
	return mapSkillToView(sk)
}
