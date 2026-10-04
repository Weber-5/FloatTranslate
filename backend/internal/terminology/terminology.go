// Package terminology keeps an in-memory snapshot of the terminology table
// and applies source→target replacements to translation input. Terminology is
// a hard constraint in the translation prompt (docs/06 §3) and participates
// in the translation config hash (docs/00 §6).
package terminology

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
	"github.com/Weber-5/FloatTranslate/backend/internal/ulcid"
)

// Service combines the table-backed repository with an in-memory snapshot
// used on the hot translation path.
type Service struct {
	repo *repository.TerminologyRepo

	mu       sync.RWMutex
	snapshot []repository.TerminologyRow // sorted by source (NOCASE)
}

// NewService builds the service and loads the initial snapshot.
func NewService(ctx context.Context, repo *repository.TerminologyRepo) (*Service, error) {
	s := &Service{repo: repo}
	if err := s.Reload(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Reload refreshes the in-memory snapshot from the repository.
func (s *Service) Reload(ctx context.Context) error {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.snapshot = rows
	s.mu.Unlock()
	return nil
}

// Snapshot returns a copy of the current terminology entries sorted by
// source (case-insensitive).
func (s *Service) Snapshot() []repository.TerminologyRow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]repository.TerminologyRow, len(s.snapshot))
	copy(out, s.snapshot)
	return out
}

// Apply replaces every terminology source occurrence in text with its target
// (case-insensitive, whole-match). Longer sources are applied first so
// overlapping entries resolve deterministically. It returns the rewritten
// text and the entries that were applied.
func (s *Service) Apply(text string) (string, []repository.TerminologyRow) {
	terms := s.Snapshot()
	if text == "" || len(terms) == 0 {
		return text, nil
	}
	sorted := make([]repository.TerminologyRow, len(terms))
	copy(sorted, terms)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(sorted[i].Source) > len(sorted[j].Source)
	})
	var applied []repository.TerminologyRow
	for _, t := range sorted {
		re, err := compileTermPattern(t.Source)
		if err != nil {
			continue
		}
		if re.MatchString(text) {
			text = re.ReplaceAllLiteralString(text, t.Target)
			applied = append(applied, t)
		}
	}
	return text, applied
}

// FindViolations is the terminology post-check (docs/09): it reports every
// applied term whose source term still occurs (case-insensitive whole-match)
// in the translated output, i.e. the model did not use the required target.
// Called with the masked (placeholder-substituted) payload so protected
// content cannot trigger false violations.
func FindViolations(output string, applied []repository.TerminologyRow) []repository.TerminologyRow {
	if output == "" || len(applied) == 0 {
		return nil
	}
	var violated []repository.TerminologyRow
	for _, t := range applied {
		re, err := compileTermPattern(t.Source)
		if err != nil {
			continue
		}
		if re.MatchString(output) {
			violated = append(violated, t)
		}
	}
	return violated
}

// compileTermPattern builds a case-insensitive whole-match pattern for a
// source term. Word boundaries are only enforced where the term starts/ends
// with a word character.
func compileTermPattern(source string) (*regexp.Regexp, error) {
	quoted := regexp.QuoteMeta(source)
	prefix := ""
	if startsWithWordChar(source) {
		prefix = `\b`
	}
	suffix := ""
	if endsWithWordChar(source) {
		suffix = `\b`
	}
	return regexp.Compile(`(?i)` + prefix + quoted + suffix)
}

func startsWithWordChar(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func endsWithWordChar(s string) bool {
	if s == "" {
		return false
	}
	return startsWithWordChar(s[len(s)-1:])
}

// Create adds a terminology entry and refreshes the snapshot.
func (s *Service) Create(ctx context.Context, source, target string) (repository.TerminologyRow, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	row := repository.TerminologyRow{
		ID:        ulcid.New(),
		Source:    strings.TrimSpace(source),
		Target:    strings.TrimSpace(target),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return repository.TerminologyRow{}, err
	}
	if err := s.Reload(ctx); err != nil {
		return repository.TerminologyRow{}, err
	}
	return row, nil
}

// Update modifies an entry and refreshes the snapshot.
func (s *Service) Update(ctx context.Context, id, source, target string) (repository.TerminologyRow, error) {
	row := repository.TerminologyRow{
		ID:        id,
		Source:    strings.TrimSpace(source),
		Target:    strings.TrimSpace(target),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.repo.Update(ctx, row); err != nil {
		return repository.TerminologyRow{}, err
	}
	updated, err := s.repo.Get(ctx, id)
	if err != nil {
		return repository.TerminologyRow{}, err
	}
	if err := s.Reload(ctx); err != nil {
		return repository.TerminologyRow{}, err
	}
	return updated, nil
}

// Delete removes an entry and refreshes the snapshot.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	return s.Reload(ctx)
}
