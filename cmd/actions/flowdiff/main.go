package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxDiffChars    = 12000
	maxErrorChars   = 4000
	maxCommentChars = 60000
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var baseSHA string
	var headSHA string
	var workspace string
	var outputFile string
	var commentFile string
	var flow2apexBin string

	flag.StringVar(&baseSHA, "base-sha", os.Getenv("BASE_SHA"), "base commit sha")
	flag.StringVar(&headSHA, "head-sha", os.Getenv("HEAD_SHA"), "head commit sha")
	flag.StringVar(&workspace, "workspace", os.Getenv("GITHUB_WORKSPACE"), "workspace path")
	flag.StringVar(&outputFile, "output-file", os.Getenv("GITHUB_OUTPUT"), "step output file path")
	flag.StringVar(&commentFile, "comment-file", "", "comment markdown output path")
	flag.StringVar(&flow2apexBin, "flow2apex-bin", os.Getenv("FLOW2APEX_BIN"), "path to flow2apex binary")
	flag.Parse()

	if baseSHA == "" || headSHA == "" {
		return fmt.Errorf("base-sha and head-sha are required")
	}
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get cwd: %w", err)
		}
		workspace = cwd
	}
	if outputFile == "" {
		outputFile = filepath.Join(workspace, ".github", "flow2apex-step-output.txt")
	}
	if commentFile == "" {
		commentFile = filepath.Join(workspace, ".github", "flow2apex-pr-comment.md")
	}

	if err := os.MkdirAll(filepath.Dir(commentFile), 0o755); err != nil {
		return fmt.Errorf("create comment directory: %w", err)
	}

	flows, err := detectChangedFlows(workspace, baseSHA, headSHA)
	if err != nil {
		return err
	}
	if len(flows) == 0 {
		if err := os.WriteFile(commentFile, []byte{}, 0o644); err != nil {
			return fmt.Errorf("write empty comment file: %w", err)
		}
		return appendOutputs(outputFile, []outputKV{
			{Key: "has_flow_changes", Value: "false"},
			{Key: "comment_file", Value: commentFile},
		})
	}

	flow2apexBin, err = resolveFlow2ApexBin(flow2apexBin)
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "flow2apex-diff-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	var comment strings.Builder
	comment.WriteString("<!-- flow2apex-diff-comment -->\n")
	comment.WriteString("## flow2apex Flow Diffs\n\n")
	comment.WriteString(fmt.Sprintf("Compared generated Apex between base `%s` and head `%s` for changed flow files.\n\n", baseSHA, headSHA))

	for _, flowPath := range flows {
		safe := sanitizeFlowPath(flowPath)
		baseDir := filepath.Join(tmpDir, "base-render-"+safe)
		headDir := filepath.Join(tmpDir, "head-render-"+safe)
		if err := os.MkdirAll(baseDir, 0o755); err != nil {
			return fmt.Errorf("create base render dir: %w", err)
		}
		if err := os.MkdirAll(headDir, 0o755); err != nil {
			return fmt.Errorf("create head render dir: %w", err)
		}

		baseStatus, baseLog, err := renderFlow(workspace, flow2apexBin, baseSHA, flowPath, filepath.Join(tmpDir, "base-"+safe+".flow"), baseDir)
		if err != nil {
			return err
		}
		headStatus, headLog, err := renderFlow(workspace, flow2apexBin, headSHA, flowPath, filepath.Join(tmpDir, "head-"+safe+".flow"), headDir)
		if err != nil {
			return err
		}

		comment.WriteString(fmt.Sprintf("### `%s`\n\n", flowPath))
		if baseStatus == 1 || headStatus == 1 {
			comment.WriteString("Conversion issues:\n\n")
			if baseStatus == 1 {
				comment.WriteString("- Base conversion failed\n")
			} else if baseStatus == 2 {
				comment.WriteString("- Base flow file missing (added in PR)\n")
			}
			if headStatus == 1 {
				comment.WriteString("- Head conversion failed\n")
			} else if headStatus == 2 {
				comment.WriteString("- Head flow file missing (deleted in PR)\n")
			}
			comment.WriteString("\n")
			if len(baseLog) > 0 || len(headLog) > 0 {
				comment.WriteString("```text\n")
				if len(baseLog) > 0 {
					comment.WriteString("[base]\n")
					comment.Write(truncateBytes(baseLog, maxErrorChars))
					comment.WriteString("\n")
				}
				if len(headLog) > 0 {
					comment.WriteString("[head]\n")
					comment.Write(truncateBytes(headLog, maxErrorChars))
					comment.WriteString("\n")
				}
				comment.WriteString("```\n\n")
			}
		}

		diffExit, diffText, err := diffRenderedOutputs(workspace, flowPath, baseDir, headDir)
		if err != nil {
			return err
		}
		switch diffExit {
		case 1:
			diffText = truncateDiff(diffText)
			comment.WriteString("```diff\n")
			comment.WriteString(diffText)
			if !strings.HasSuffix(diffText, "\n") {
				comment.WriteString("\n")
			}
			comment.WriteString("```\n\n")
		case 0:
			comment.WriteString("No generated Apex differences.\n\n")
		default:
			comment.WriteString("Failed to generate diff output.\n\n")
		}
	}

	commentBody := comment.String()
	if len(commentBody) > maxCommentChars {
		commentBody = commentBody[:maxCommentChars] + "\n...comment truncated due to size limit...\n"
	}
	if err := os.WriteFile(commentFile, []byte(commentBody), 0o644); err != nil {
		return fmt.Errorf("write comment file: %w", err)
	}

	return appendOutputs(outputFile, []outputKV{
		{Key: "has_flow_changes", Value: "true"},
		{Key: "comment_file", Value: commentFile},
	})
}

func detectChangedFlows(workspace, baseSHA, headSHA string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "--no-renames", "--diff-filter=ACMRD", baseSHA, headSHA)
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("detect changed files: %w", err)
	}
	re := regexp.MustCompile(`\.flow(-meta\.xml)?$`)

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	flows := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if re.MatchString(line) {
			flows = append(flows, line)
		}
	}
	sort.Strings(flows)
	return dedupe(flows), nil
}

func dedupe(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := make([]string, 0, len(in))
	prev := ""
	for i, s := range in {
		if i == 0 || s != prev {
			out = append(out, s)
		}
		prev = s
	}
	return out
}

func resolveFlow2ApexBin(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		value = "flow2apex"
	}
	if strings.Contains(value, "/") {
		info, err := os.Stat(value)
		if err != nil {
			return "", fmt.Errorf("FLOW2APEX_BIN is not executable: %s", value)
		}
		if info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("FLOW2APEX_BIN is not executable: %s", value)
		}
		return value, nil
	}
	resolved, err := exec.LookPath(value)
	if err != nil {
		return "", fmt.Errorf("flow2apex binary not found on PATH")
	}
	return resolved, nil
}

func gitObjectExists(workspace, sha, flowPath string) bool {
	cmd := exec.Command("git", "cat-file", "-e", sha+":"+flowPath)
	cmd.Dir = workspace
	return cmd.Run() == nil
}

func gitShow(workspace, sha, flowPath string) ([]byte, error) {
	cmd := exec.Command("git", "show", sha+":"+flowPath)
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read flow contents %s (%s): %w", flowPath, sha, err)
	}
	return out, nil
}

func renderFlow(workspace, flow2apexBin, sha, flowPath, flowFilePath, outputDir string) (int, []byte, error) {
	if !gitObjectExists(workspace, sha, flowPath) {
		return 2, nil, nil
	}
	contents, err := gitShow(workspace, sha, flowPath)
	if err != nil {
		return 1, nil, err
	}
	if err := os.WriteFile(flowFilePath, contents, 0o644); err != nil {
		return 1, nil, fmt.Errorf("write temp flow file: %w", err)
	}

	var log bytes.Buffer
	ok, stderr, err := runFlow2ApexToDir(workspace, flow2apexBin, flowFilePath, outputDir)
	if err != nil {
		return 1, nil, err
	}
	log.Write(stderr)
	if ok {
		return 0, log.Bytes(), nil
	}

	ok, stdout, stderr, err := runFlow2ApexToStdout(workspace, flow2apexBin, flowFilePath)
	if err != nil {
		return 1, nil, err
	}
	log.Write(stderr)
	if ok {
		if err := os.WriteFile(filepath.Join(outputDir, "generated.apex"), stdout, 0o644); err != nil {
			return 1, nil, fmt.Errorf("write generated apex fallback: %w", err)
		}
		return 0, log.Bytes(), nil
	}
	return 1, log.Bytes(), nil
}

func runFlow2ApexToDir(workspace, bin, flowFile, outputDir string) (bool, []byte, error) {
	cmd := exec.Command(bin, flowFile, "-d", outputDir)
	cmd.Dir = workspace
	var stderr bytes.Buffer
	cmd.Stdout = bytes.NewBuffer(nil)
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, stderr.Bytes(), nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		return false, stderr.Bytes(), nil
	}
	return false, nil, fmt.Errorf("run flow2apex with output-dir: %w", err)
}

func runFlow2ApexToStdout(workspace, bin, flowFile string) (bool, []byte, []byte, error) {
	cmd := exec.Command(bin, flowFile)
	cmd.Dir = workspace
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, stdout.Bytes(), stderr.Bytes(), nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		return false, stdout.Bytes(), stderr.Bytes(), nil
	}
	return false, nil, nil, fmt.Errorf("run flow2apex fallback: %w", err)
}

func diffRenderedOutputs(workspace, flowPath, baseDir, headDir string) (int, string, error) {
	cmd := exec.Command(
		"git",
		"diff",
		"--no-index",
		"--src-prefix=a/"+flowPath+"/",
		"--dst-prefix=b/"+flowPath+"/",
		"--",
		baseDir,
		headDir,
	)
	cmd.Dir = workspace
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), stdout.String(), nil
	}
	return 2, "", fmt.Errorf("generate diff output: %w", err)
}

func truncateDiff(diffText string) string {
	if len(diffText) <= maxDiffChars {
		return diffText
	}
	return diffText[:maxDiffChars] + "\n...diff truncated..."
}

func truncateBytes(data []byte, max int) []byte {
	if len(data) <= max {
		return data
	}
	return data[:max]
}

func sanitizeFlowPath(flowPath string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		" ", "_",
		"\t", "_",
		"\n", "_",
		":", "_",
	)
	return replacer.Replace(flowPath)
}

type outputKV struct {
	Key   string
	Value string
}

func appendOutputs(path string, values []outputKV) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open output file: %w", err)
	}
	defer f.Close()
	for _, kv := range values {
		if _, err := fmt.Fprintf(f, "%s=%s\n", kv.Key, kv.Value); err != nil {
			return fmt.Errorf("write output %s: %w", kv.Key, err)
		}
	}
	return nil
}
