package app

import (
	"fmt"
	"os"

	"github.com/amin-jalali/termocode/internal/ai"
)

// loadProfiles resolves the model slots (config.json ai_models plus the
// built-in main / quick / local) and loads the agent profiles. It never
// fails: bad slots and broken profile files land in profiles.Errors and
// the built-in profiles stay.
func (s *aiState) loadProfiles(creds ai.Credentials) {
	s.providers = loadAIProviders(s.settings, creds)
	s.modelsCache = nil // read again with the providers (Add AI wrote it)
	cfgs, errs := ai.ResolveSlots(s.settings, s.cfg, creds, os.Getenv)
	limits, problems := s.settings.ConcurrencyLimits() // OMX-26
	ai.SetConcurrencyLimits(limits)
	errs = append(errs, problems...)
	s.slots = ai.NewSlotPool(cfgs, nil).WithSynthetic(ai.SyntheticResolver(s.providers, s.cfg, creds, os.Getenv))
	known := map[string]bool{ai.SlotMain: true, ai.SlotQuick: true, ai.SlotLocal: true}
	for n := range cfgs {
		known[n] = true
	}
	s.slotKnown, s.slotErrs = known, errs
	s.reloadProfiles()
}

// loadAIProviders reads ai/providers.json. Without the file the list is
// derived from the setup (stored keys, a local ai_provider) and kept in
// memory only: whether a CLI is signed in takes a process to find out,
// so a list with the CLIs in it is written later, off the UI path.
func loadAIProviders(st ai.Settings, creds ai.Credentials) map[string]ai.ProviderEntry {
	path, err := ai.ProvidersPath()
	if err == nil {
		if ps, err := ai.LoadProviders(path); err == nil {
			return ps
		}
	}
	return ai.DeriveProviders(st, creds, nil)
}

// reloadProfiles reads profiles.json and the project's ai-profiles.json
// again (the slots stay): the picker and the cycle key see an edit at
// once.
func (s *aiState) reloadProfiles() {
	known := s.slotKnown
	if known == nil {
		known = map[string]bool{ai.SlotMain: true, ai.SlotQuick: true, ai.SlotLocal: true}
	}
	user, _ := ai.ProfilesPath()
	s.profiles = ai.LoadProfiles(user, ai.ProjectProfilesPath(aiWorkspaceRoot()), known, s.settings.Parallel())
	s.profiles.Errors = append(append([]string(nil), s.slotErrs...), s.profiles.Errors...)
	for _, p := range s.profFake {
		s.profiles.Add(p)
	}
	// Work until done: the settings' rounds and budget for the loop
	// profiles that leave them out (a bad value falls back and is named).
	rounds, budget, problems := s.settings.UntilDone()
	s.profiles.SetLoopDefaults(rounds, budget)
	s.profiles.Errors = append(s.profiles.Errors, problems...)
}

// slotProvider returns the provider of a model slot. "main" (or "") is
// the chat's own provider, so the default profile is today's chat; the
// others are built on first use.
func (s *aiState) slotProvider(name string) (ai.Provider, ai.Config, error) {
	if name == "" || name == ai.SlotMain {
		if s.provider == nil || !s.provider.Configured() {
			return nil, s.cfg, fmt.Errorf("model slot %q: %w", ai.SlotMain, ai.ErrNotConfigured)
		}
		return s.provider, s.cfg, nil
	}
	if s.slots == nil {
		return nil, ai.Config{}, fmt.Errorf("unknown model slot %q", name)
	}
	return s.slots.Provider(name)
}
