package git_repo

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-git/go-git/v5"

	"github.com/werf/logboek"

	"github.com/werf/werf/pkg/git_repo/check_ignore"
	"github.com/werf/werf/pkg/git_repo/status"
	"github.com/werf/werf/pkg/path_matcher"
	"github.com/werf/werf/pkg/true_git"
	"github.com/werf/werf/pkg/true_git/ls_tree"
	"github.com/werf/werf/pkg/util"
)

type Local struct {
	Base
	Path   string
	GitDir string
}

func OpenLocalRepo(name string, path string) (*Local, error) {
	_, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
	if err != nil {
		if err == git.ErrRepositoryNotExists {
			return nil, nil
		}

		return nil, err
	}

	gitDir, err := true_git.GetRealRepoDir(filepath.Join(path, ".git"))
	if err != nil {
		return nil, fmt.Errorf("unable to get real git repo dir for %s: %s", path, err)
	}

	localRepo := &Local{Base: Base{Name: name}, Path: path, GitDir: gitDir}

	return localRepo, nil
}

func (repo *Local) PlainOpen() (*git.Repository, error) {
	return git.PlainOpen(repo.Path)
}

func (repo *Local) SyncWithOrigin(ctx context.Context) error {
	isShallow, err := repo.IsShallowClone()
	if err != nil {
		return fmt.Errorf("check shallow clone failed: %s", err)
	}

	remoteOriginUrl, err := repo.RemoteOriginUrl(ctx)
	if err != nil {
		return fmt.Errorf("get remote origin failed: %s", err)
	}

	if remoteOriginUrl == "" {
		return fmt.Errorf("git remote origin was not detected")
	}

	return logboek.Context(ctx).Default().LogProcess("Syncing origin branches and tags").DoError(func() error {
		fetchOptions := true_git.FetchOptions{
			Prune:     true,
			PruneTags: true,
			Unshallow: isShallow,
			RefSpecs:  map[string]string{"origin": "+refs/heads/*:refs/remotes/origin/*"},
		}

		if err := true_git.Fetch(ctx, repo.Path, fetchOptions); err != nil {
			return fmt.Errorf("fetch failed: %s", err)
		}

		return nil
	})
}

func (repo *Local) FetchOrigin(ctx context.Context) error {
	isShallow, err := repo.IsShallowClone()
	if err != nil {
		return fmt.Errorf("check shallow clone failed: %s", err)
	}

	remoteOriginUrl, err := repo.RemoteOriginUrl(ctx)
	if err != nil {
		return fmt.Errorf("get remote origin failed: %s", err)
	}

	if remoteOriginUrl == "" {
		return fmt.Errorf("git remote origin was not detected")
	}

	return logboek.Context(ctx).Default().LogProcess("Fetching origin").DoError(func() error {
		fetchOptions := true_git.FetchOptions{
			Unshallow: isShallow,
			RefSpecs:  map[string]string{"origin": "+refs/heads/*:refs/remotes/origin/*"},
		}

		if err := true_git.Fetch(ctx, repo.Path, fetchOptions); err != nil {
			return fmt.Errorf("fetch failed: %s", err)
		}

		return nil
	})
}

func (repo *Local) IsShallowClone() (bool, error) {
	return true_git.IsShallowClone(repo.Path)
}

func (repo *Local) CreateDetachedMergeCommit(ctx context.Context, fromCommit, toCommit string) (string, error) {
	return repo.createDetachedMergeCommit(ctx, repo.GitDir, repo.Path, repo.getRepoWorkTreeCacheDir(), fromCommit, toCommit)
}

func (repo *Local) GetMergeCommitParents(_ context.Context, commit string) ([]string, error) {
	return repo.getMergeCommitParents(repo.GitDir, commit)
}

type LsTreeOptions struct {
	Commit        string
	UseHeadCommit bool
	Strict        bool
}

func (repo *Local) LsTree(ctx context.Context, pathMatcher path_matcher.PathMatcher, opts LsTreeOptions) (*ls_tree.Result, error) {
	repository, err := git.PlainOpenWithOptions(repo.Path, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
	if err != nil {
		return nil, fmt.Errorf("cannot open repo %s: %s", repo.Path, err)
	}

	var commit string
	if opts.UseHeadCommit {
		if headCommit, err := repo.HeadCommit(ctx); err != nil {
			return nil, fmt.Errorf("unable to get repo head commit: %s", err)
		} else {
			commit = headCommit
		}
	} else if opts.Commit == "" {
		panic(fmt.Sprintf("no commit specified for LsTree procedure: specify Commit or HeadCommit"))
	} else {
		commit = opts.Commit
	}

	return ls_tree.LsTree(ctx, repository, commit, pathMatcher, opts.Strict)
}

func (repo *Local) Status(ctx context.Context, pathMatcher path_matcher.PathMatcher) (*status.Result, error) {
	repository, err := git.PlainOpenWithOptions(repo.Path, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
	if err != nil {
		return nil, fmt.Errorf("cannot open repo %s: %s", repo.Path, err)
	}

	return status.Status(ctx, repository, repo.Path, pathMatcher)
}

func (repo *Local) CheckIgnore(ctx context.Context, paths []string) (*check_ignore.Result, error) {
	repository, err := git.PlainOpenWithOptions(repo.Path, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
	if err != nil {
		return nil, fmt.Errorf("cannot open repo %s: %s", repo.Path, err)
	}

	return check_ignore.CheckIgnore(ctx, repository, repo.Path, paths)
}

func (repo *Local) IsEmpty(ctx context.Context) (bool, error) {
	return repo.isEmpty(ctx, repo.Path)
}

func (repo *Local) IsAncestor(_ context.Context, ancestorCommit, descendantCommit string) (bool, error) {
	return true_git.IsAncestor(ancestorCommit, descendantCommit, repo.GitDir)
}

func (repo *Local) RemoteOriginUrl(ctx context.Context) (string, error) {
	return repo.remoteOriginUrl(repo.Path)
}

func (repo *Local) HeadCommit(ctx context.Context) (string, error) {
	return repo.getHeadCommit(repo.Path)
}

func (repo *Local) IsHeadReferenceExist(ctx context.Context) (bool, error) {
	_, err := repo.getHeadCommit(repo.Path)
	if err == errHeadNotFound {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func (repo *Local) CreatePatch(ctx context.Context, opts PatchOptions) (Patch, error) {
	return repo.createPatch(ctx, repo.Path, repo.GitDir, repo.getRepoWorkTreeCacheDir(), opts)
}

func (repo *Local) CreateArchive(ctx context.Context, opts ArchiveOptions) (Archive, error) {
	return repo.createArchive(ctx, repo.Path, repo.GitDir, repo.getRepoWorkTreeCacheDir(), opts)
}

func (repo *Local) Checksum(ctx context.Context, opts ChecksumOptions) (checksum Checksum, err error) {
	logboek.Context(ctx).Debug().LogProcess("Calculating checksum").Do(func() {
		checksum, err = repo.checksumWithLsTree(ctx, repo.Path, repo.GitDir, repo.getRepoWorkTreeCacheDir(), opts)
	})

	return checksum, err
}

func (repo *Local) IsCommitExists(ctx context.Context, commit string) (bool, error) {
	return repo.isCommitExists(ctx, repo.Path, repo.GitDir, commit)
}

func (repo *Local) TagsList(ctx context.Context) ([]string, error) {
	return repo.tagsList(repo.Path)
}

func (repo *Local) RemoteBranchesList(ctx context.Context) ([]string, error) {
	return repo.remoteBranchesList(repo.Path)
}

func (repo *Local) getRepoWorkTreeCacheDir() string {
	absPath, err := filepath.Abs(repo.Path)
	if err != nil {
		panic(err) // stupid interface of filepath.Abs
	}

	fullPath := filepath.Clean(absPath)
	repoId := util.Sha256Hash(fullPath)

	return filepath.Join(GetWorkTreeCacheDir(), "local", repoId)
}

func (repo *Local) IsFileExists(commit, path string) (bool, error) {
	panic("NO")
}

func (repo *Local) GetFilePathList(commit string) ([]string, error) {
	panic("NO")
}

func (repo *Local) ReadFile(commit, filePath string) ([]byte, error) {
	return readFile(repo.Path, commit, filePath)
}

func readFile(repoPath, commit, filePath string) ([]byte, error) {
	repository, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
	if err != nil {
		return nil, fmt.Errorf("cannot open repo %s: %s", repoPath, err)
	}

	commitHash, err := newHash(commit)
	if err != nil {
		return nil, fmt.Errorf("bad commit hash %q: %s", commit, err)
	}

	commitObj, err := repository.CommitObject(commitHash)
	if err != nil {
		return nil, fmt.Errorf("cannot get commit %q object: %s", commit, err)
	}

	file, err := commitObj.File(filePath)
	if err != nil {
		return nil, fmt.Errorf("error getting repo file %q from commit %q: %s", filePath, commit, err)
	}

	content, err := file.Contents()
	if err != nil {
		return nil, err
	}

	return []byte(content), nil
}
