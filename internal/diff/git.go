package diff

import (
	"os/exec"
)

// RunGitDiff runs `git diff` for the given ref range in repoDir.
func RunGitDiff(repoDir, baseRef, headRef string) (string, error) {
	args := []string{"-C", repoDir, "diff", "--no-color", "-U999999"}
	if baseRef != "" && headRef != "" {
		args = append(args, baseRef+".."+headRef)
	} else if headRef != "" {
		args = append(args, headRef)
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// RunGitDiffStaged returns staged (cached) diffs only.
func RunGitDiffStaged(repoDir string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "diff", "--staged", "--no-color", "-U999999")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
