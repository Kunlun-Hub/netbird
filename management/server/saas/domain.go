package saas

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/netbirdio/netbird/management/server/store"
	nbdomain "github.com/netbirdio/netbird/shared/management/domain"
	"github.com/netbirdio/netbird/shared/management/status"
)

const (
	DefaultSlugMinLength = 8
	DefaultSlugMaxLength = 12
	defaultSlugAttempts  = 32
	slugAlphabet         = "abcdefghijklmnopqrstuvwxyz0123456789"
)

var (
	ErrDomainSuffixRequired = errors.New("organization domain suffix is required")
	ErrSlugUnavailable      = errors.New("failed to allocate unique organization slug")

	reservedSlugs = map[string]struct{}{
		"admin":     {},
		"api":       {},
		"www":       {},
		"login":     {},
		"root":      {},
		"support":   {},
		"dashboard": {},
		"saas":      {},
	}
)

type DomainAllocator struct {
	Store   store.Store
	Suffix  string
	MinSize int
	MaxSize int
}

type OrganizationDomain struct {
	Slug   string
	Domain string
}

func (a DomainAllocator) Allocate(ctx context.Context) (OrganizationDomain, error) {
	suffix := normalizeDomainSuffix(a.Suffix)
	if suffix == "" {
		return OrganizationDomain{}, ErrDomainSuffixRequired
	}

	minSize, maxSize := a.slugBounds()
	for i := 0; i < defaultSlugAttempts; i++ {
		slug, err := randomSlug(randomLength(minSize, maxSize))
		if err != nil {
			return OrganizationDomain{}, err
		}
		if IsReservedSlug(slug) {
			continue
		}

		domain, err := BuildOrganizationDomain(slug, suffix)
		if err != nil {
			continue
		}
		if a.Store == nil {
			return OrganizationDomain{Slug: slug, Domain: domain}, nil
		}
		if _, err := a.Store.GetSaaSOrganizationByDomain(ctx, store.LockingStrengthNone, domain); err != nil {
			if isNotFound(err) {
				return OrganizationDomain{Slug: slug, Domain: domain}, nil
			}
			return OrganizationDomain{}, err
		}
	}

	return OrganizationDomain{}, ErrSlugUnavailable
}

func (a DomainAllocator) slugBounds() (int, int) {
	minSize := a.MinSize
	maxSize := a.MaxSize
	if minSize == 0 {
		minSize = DefaultSlugMinLength
	}
	if maxSize == 0 {
		maxSize = DefaultSlugMaxLength
	}
	if minSize < DefaultSlugMinLength {
		minSize = DefaultSlugMinLength
	}
	if maxSize < minSize {
		maxSize = minSize
	}
	return minSize, maxSize
}

func IsReservedSlug(slug string) bool {
	_, ok := reservedSlugs[strings.ToLower(slug)]
	return ok
}

func BuildOrganizationDomain(slug, suffix string) (string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	suffix = normalizeDomainSuffix(suffix)
	if suffix == "" {
		return "", ErrDomainSuffixRequired
	}
	if !ValidSlug(slug) || IsReservedSlug(slug) {
		return "", fmt.Errorf("invalid organization slug: %s", slug)
	}
	domain := slug + "." + suffix
	if !nbdomain.IsValidDomainNoWildcard(domain) {
		return "", fmt.Errorf("invalid organization domain: %s", domain)
	}
	return domain, nil
}

func ValidSlug(slug string) bool {
	if len(slug) < DefaultSlugMinLength || len(slug) > 63 {
		return false
	}
	for _, r := range slug {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func randomSlug(length int) (string, error) {
	buf := make([]byte, length)
	random := make([]byte, length)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for i, b := range random {
		buf[i] = slugAlphabet[int(b)%len(slugAlphabet)]
	}
	return string(buf), nil
}

func randomLength(minSize, maxSize int) int {
	if maxSize <= minSize {
		return minSize
	}
	var b [1]byte
	if _, err := rand.Read(b[:]); err != nil {
		return minSize
	}
	return minSize + int(b[0])%(maxSize-minSize+1)
}

func normalizeDomainSuffix(suffix string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(suffix)), ".")
}

func isNotFound(err error) bool {
	var statusErr *status.Error
	return errors.As(err, &statusErr) && statusErr.Type() == status.NotFound
}
