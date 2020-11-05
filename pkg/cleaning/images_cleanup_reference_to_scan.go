package cleaning

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/werf/logboek"

	"github.com/werf/werf/pkg/config"
)

type referenceToScan struct {
	*plumbing.Reference
	CreatedAt  time.Time
	HeadCommit *object.Commit
	referenceScanOptions
}

type referenceScanOptions struct {
	scanDepthLimit          int
	imagesCleanupKeepPolicy config.MetaCleanupKeepPolicyImagesPerReference
}

func (r *referenceToScan) String() string {
	imagesCleanupKeepPolicy := r.imagesCleanupKeepPolicy.String()
	if imagesCleanupKeepPolicy != "" {
		imagesCleanupKeepPolicy = fmt.Sprintf(" (%s)", imagesCleanupKeepPolicy)
	}

	return fmt.Sprintf("%s%s", r.Name().Short(), imagesCleanupKeepPolicy)
}

func getReferencesToScan(ctx context.Context, gitRepository *git.Repository, keepPolicies []*config.MetaCleanupKeepPolicy) ([]*referenceToScan, error) {
	rs, err := gitRepository.References()
	if err != nil {
		return nil, fmt.Errorf("get repository references failed: %s", err)
	}

	var refs []*referenceToScan
	if err := rs.ForEach(func(reference *plumbing.Reference) error {
		n := reference.Name()

		// Get all remote branches and tags
		if !(n.IsRemote() || n.IsTag()) {
			return nil
		}

		// Use only origin upstream
		if n.IsRemote() && !strings.HasPrefix(n.Short(), "origin/") {
			return nil
		}

		var scanDepthLimit int
		var modifiedAt time.Time
		var refCommit *object.Commit
		if !n.IsTag() {
			scanDepthLimit = -1 // unlimited

			refHash := reference.Hash()
			if refHash == plumbing.ZeroHash {
				return nil
			}

			refCommit, err = gitRepository.CommitObject(refHash)
			if err != nil {
				return fmt.Errorf("reference %s: commit object %s failed: %s", n.Short(), refHash.String(), err)
			}

			modifiedAt = refCommit.Committer.When
		} else {
			scanDepthLimit = 1

			revHash, err := gitRepository.ResolveRevision(plumbing.Revision(reference.Hash().String()))
			if err != nil {
				return fmt.Errorf("resolve revision %s failed: %s", n.Short(), err)
			}

			refCommit, err = gitRepository.CommitObject(*revHash)
			if err != nil {
				return fmt.Errorf("reference %s: commit object %s failed: %s", n.Short(), revHash.String(), err)
			}

			tagObject, err := gitRepository.TagObject(reference.Hash())
			if err == nil {
				modifiedAt = tagObject.Tagger.When
			} else if err == plumbing.ErrObjectNotFound { // lightweight tag
				modifiedAt = refCommit.Committer.When
			} else {
				return fmt.Errorf("tag object %s failed: %s", reference.Hash(), err)
			}
		}

		refs = append(refs, &referenceToScan{
			Reference:  reference,
			CreatedAt:  modifiedAt,
			HeadCommit: refCommit,
			referenceScanOptions: referenceScanOptions{
				scanDepthLimit: scanDepthLimit,
			},
		})

		return nil
	}); err != nil {
		return nil, err
	}

	// Split branches and tags references
	var branchesRefs, tagsRefs []*referenceToScan
	for _, ref := range refs {
		if ref.Name().IsTag() {
			tagsRefs = append(tagsRefs, ref)
		} else {
			branchesRefs = append(branchesRefs, ref)
		}
	}

	// Apply user or default policies
	if len(keepPolicies) == 0 {
		tagLast := 10
		keepPolicies = append(keepPolicies, &config.MetaCleanupKeepPolicy{
			References: config.MetaCleanupKeepPolicyReferences{
				TagRegexp: regexp.MustCompile(".*"),
				Limit: &config.MetaCleanupKeepPolicyLimit{
					Last: &tagLast,
				},
			},
		})

		branchLast := 10
		branchIn := time.Hour * 24 * 7
		branchImagesPerReferenceLast := 2
		branchImagesPerReferenceIn := time.Hour * 24 * 7
		keepPolicies = append(keepPolicies, &config.MetaCleanupKeepPolicy{
			References: config.MetaCleanupKeepPolicyReferences{
				BranchRegexp: regexp.MustCompile(".*"),
				Limit: &config.MetaCleanupKeepPolicyLimit{
					Last:     &branchLast,
					In:       &branchIn,
					Operator: &config.AndOperator,
				},
			},
			ImagesPerReference: config.MetaCleanupKeepPolicyImagesPerReference{
				MetaCleanupKeepPolicyLimit: config.MetaCleanupKeepPolicyLimit{
					Last:     &branchImagesPerReferenceLast,
					In:       &branchImagesPerReferenceIn,
					Operator: &config.AndOperator,
				},
			},
		})

		mainBranchImagesPerReferenceLast := 10
		keepPolicies = append(keepPolicies, &config.MetaCleanupKeepPolicy{
			References: config.MetaCleanupKeepPolicyReferences{
				BranchRegexp: regexp.MustCompile("^(master|staging|production)$"),
			},
			ImagesPerReference: config.MetaCleanupKeepPolicyImagesPerReference{
				MetaCleanupKeepPolicyLimit: config.MetaCleanupKeepPolicyLimit{
					Last: &mainBranchImagesPerReferenceLast,
				},
			},
		})
	}

	var resultTagsRefs, resultBranchesRefs []*referenceToScan
	for _, policy := range keepPolicies {
		var policyRefs []*referenceToScan

		if policy.References.BranchRegexp != nil {
			policyRefs = selectBranchReferencesByRegexp(branchesRefs, policy.References.BranchRegexp)
			policyRefs = applyCleanupKeepPolicy(policyRefs, policy)
			resultBranchesRefs = mergeReferences(resultBranchesRefs, policyRefs)
		} else if policy.References.TagRegexp != nil {
			policyRefs = selectTagReferencesByRegexp(tagsRefs, policy.References.TagRegexp)
			policyRefs = applyCleanupKeepPolicy(policyRefs, policy)
			resultTagsRefs = mergeReferences(resultTagsRefs, policyRefs)
		}

		logboek.Context(ctx).Default().LogBlock(policy.String()).Do(func() {
			for _, ref := range policyRefs {
				logboek.Context(ctx).Default().LogLnDetails(ref.Name().Short())
			}
		})
	}

	// Sort by ModifiedAt
	sort.Slice(resultBranchesRefs, func(i, j int) bool {
		return resultBranchesRefs[i].CreatedAt.After(resultBranchesRefs[j].CreatedAt)
	})
	sort.Slice(resultTagsRefs, func(i, j int) bool {
		return resultTagsRefs[i].CreatedAt.After(resultTagsRefs[j].CreatedAt)
	})

	// Unite tags and branches references
	result := append(resultBranchesRefs, resultTagsRefs...)

	return result, nil
}

func selectBranchReferencesByRegexp(branchesRefs []*referenceToScan, regexp *regexp.Regexp) []*referenceToScan {
	var result []*referenceToScan

	for _, branchRef := range branchesRefs {
		refShortNameWithoutRemote := strings.SplitN(branchRef.Name().Short(), "/", 2)[1]
		if regexp.MatchString(refShortNameWithoutRemote) {
			result = append(result, branchRef)
		}
	}

	return result
}

func selectTagReferencesByRegexp(tagsRefs []*referenceToScan, regexp *regexp.Regexp) []*referenceToScan {
	var result []*referenceToScan

	for _, tagRef := range tagsRefs {
		if regexp.MatchString(tagRef.Name().Short()) {
			result = append(result, tagRef)
		}
	}

	return result
}

func applyCleanupKeepPolicy(refs []*referenceToScan, policy *config.MetaCleanupKeepPolicy) []*referenceToScan {
	refs = applyReferencesLimit(refs, policy.References.Limit)
	applyImagesPerReference(refs, policy.ImagesPerReference)

	return refs
}

func applyReferencesLimit(refs []*referenceToScan, limit *config.MetaCleanupKeepPolicyLimit) []*referenceToScan {
	if limit == nil {
		return refs
	}

	var policyInRefs []*referenceToScan
	if limit.In != nil {
		policyInRefs = filterReferencesByIn(refs, *limit.In)
	}

	var policyLastRefs []*referenceToScan
	if limit.Last != nil {
		policyLastRefs = filterReferencesByLast(refs, *limit.Last)
	}

	if limit.In == nil {
		return policyLastRefs
	} else if limit.Last == nil {
		return policyInRefs
	}

	var policyRefs []*referenceToScan
	if limit.Operator != nil && *limit.Operator == config.OrOperator {
		policyRefs = referencesOr(policyInRefs, policyLastRefs)
	} else {
		policyRefs = referencesAnd(policyInRefs, policyLastRefs)
	}

	return policyRefs
}

func applyImagesPerReference(policyBranchesRefs []*referenceToScan, imagesPerReference config.MetaCleanupKeepPolicyImagesPerReference) {
	for _, ref := range policyBranchesRefs {
		ref.imagesCleanupKeepPolicy = imagesPerReference
	}
}

func referencesOr(refs1 []*referenceToScan, refs2 []*referenceToScan) []*referenceToScan {
	return mergeReferences(refs1, refs2)
}

func referencesAnd(refs1 []*referenceToScan, refs2 []*referenceToScan) []*referenceToScan {
	var result []*referenceToScan

outerLoop:
	for _, ref1 := range refs1 {
		for _, ref2 := range refs2 {
			if ref1 == ref2 {
				result = append(result, ref1)
				continue outerLoop
			}
		}
	}

	return result
}

func filterReferencesByIn(refs []*referenceToScan, in time.Duration) (result []*referenceToScan) {
	for _, ref := range refs {
		if ref.CreatedAt.After(time.Now().Add(-in)) {
			result = append(result, ref)
		}
	}

	return
}

func filterReferencesByLast(refs []*referenceToScan, last int) []*referenceToScan {
	if last == -1 {
		return refs
	}

	sort.Slice(refs, func(i, j int) bool {
		return refs[i].CreatedAt.After(refs[j].CreatedAt)
	})

	if len(refs) < last {
		return refs
	}

	return refs[:last]
}

func mergeReferences(refs1 []*referenceToScan, refs2 []*referenceToScan) []*referenceToScan {
	result := refs2[:]

outerLoop:
	for _, ref1 := range refs1 {
		for _, ref2 := range refs2 {
			if ref1 == ref2 {
				continue outerLoop
			}
		}

		result = append(result, ref1)
	}

	return result
}
