package main

import (
	"bytes"
	"flag"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxDiffChars      = 12000
	maxErrorChars     = 4000
	maxCommentChars   = 60000
	sideBySideWidth   = 200
	sideBySideTabSize = 3

	diffFormatUnified    = "unified"
	diffFormatSideBySide = "side-by-side"
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
	var htmlFile string
	var flow2apexBin string
	var diffFormat string

	flag.StringVar(&baseSHA, "base-sha", os.Getenv("BASE_SHA"), "base commit sha")
	flag.StringVar(&headSHA, "head-sha", os.Getenv("HEAD_SHA"), "head commit sha")
	flag.StringVar(&workspace, "workspace", os.Getenv("GITHUB_WORKSPACE"), "workspace path")
	flag.StringVar(&outputFile, "output-file", os.Getenv("GITHUB_OUTPUT"), "step output file path")
	flag.StringVar(&commentFile, "comment-file", "", "comment markdown output path")
	flag.StringVar(&htmlFile, "html-file", "", "side-by-side html output path")
	flag.StringVar(&flow2apexBin, "flow2apex-bin", os.Getenv("FLOW2APEX_BIN"), "path to flow2apex binary")
	flag.StringVar(&diffFormat, "diff-format", os.Getenv("DIFF_FORMAT"), "diff format: unified or side-by-side")
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
	if htmlFile == "" {
		htmlFile = filepath.Join(workspace, ".github", "flow2apex-pr-diff.html")
	}
	resolvedDiffFormat, err := normalizeDiffFormat(diffFormat)
	if err != nil {
		return err
	}

	htmlFileOutput := ""
	if resolvedDiffFormat == diffFormatSideBySide {
		htmlFileOutput = htmlFile
	}

	if err := os.MkdirAll(filepath.Dir(commentFile), 0o755); err != nil {
		return fmt.Errorf("create comment directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(htmlFile), 0o755); err != nil {
		return fmt.Errorf("create html directory: %w", err)
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
			{Key: "html_file", Value: htmlFileOutput},
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

	baseCheckout := filepath.Join(tmpDir, "base-checkout")
	if err := createDetachedWorktree(workspace, baseSHA, baseCheckout); err != nil {
		return err
	}
	defer func() {
		if err := removeWorktree(workspace, baseCheckout); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
	}()

	headCheckout := filepath.Join(tmpDir, "head-checkout")
	if err := createDetachedWorktree(workspace, headSHA, headCheckout); err != nil {
		return err
	}
	defer func() {
		if err := removeWorktree(workspace, headCheckout); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
	}()

	var comment strings.Builder
	comment.WriteString(diffCommentMarker(resolvedDiffFormat))
	comment.WriteString("\n")
	comment.WriteString("## flow2apex Flow Diffs\n\n")
	comment.WriteString(fmt.Sprintf("Compared generated Apex between base `%s` and head `%s` for changed flow files.\n\n", baseSHA, headSHA))
	comment.WriteString(fmt.Sprintf("Diff format: `%s`.\n\n", resolvedDiffFormat))

	var sideBySideHTML strings.Builder
	if resolvedDiffFormat == diffFormatSideBySide {
		sideBySideHTML.WriteString(startSideBySideHTMLReport(baseSHA, headSHA))
	}

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

		baseStatus, baseLog, err := renderFlow(baseCheckout, flow2apexBin, flowPath, baseDir)
		if err != nil {
			return err
		}
		headStatus, headLog, err := renderFlow(headCheckout, flow2apexBin, flowPath, headDir)
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

		diffExit, diffText, err := diffRenderedOutputs(workspace, flowPath, baseDir, headDir, resolvedDiffFormat)
		if err != nil {
			return err
		}
		switch diffExit {
		case 1:
			commentDiffText := diffText
			if resolvedDiffFormat == diffFormatSideBySide {
				commentDiffText = suppressCommonSideBySideDiffLines(diffText)
			}
			if resolvedDiffFormat == diffFormatSideBySide {
				sideBySideHTML.WriteString("    <h2>")
				sideBySideHTML.WriteString(html.EscapeString(flowPath))
				sideBySideHTML.WriteString("</h2>\n")
				sideBySideHTML.WriteString("    <pre class=\"sbs\"><span class=\"sbs-scale\">")
				sideBySideHTML.WriteString(formatSideBySideDiffHTML(diffText))
				sideBySideHTML.WriteString("</span></pre>\n")
			}

			commentDiffText = truncateDiff(commentDiffText)
			if resolvedDiffFormat == diffFormatSideBySide {
				comment.WriteString("```text\n")
			} else {
				comment.WriteString("```diff\n")
			}
			comment.WriteString(commentDiffText)
			if !strings.HasSuffix(commentDiffText, "\n") {
				comment.WriteString("\n")
			}
			comment.WriteString("```\n\n")
		case 0:
			comment.WriteString("No generated Apex differences.\n\n")
			if resolvedDiffFormat == diffFormatSideBySide {
				sideBySideHTML.WriteString("    <h2>")
				sideBySideHTML.WriteString(html.EscapeString(flowPath))
				sideBySideHTML.WriteString("</h2>\n")
				sideBySideHTML.WriteString("    <p>No generated Apex differences.</p>\n")
			}
		default:
			comment.WriteString("Failed to generate diff output.\n\n")
			if resolvedDiffFormat == diffFormatSideBySide {
				sideBySideHTML.WriteString("    <h2>")
				sideBySideHTML.WriteString(html.EscapeString(flowPath))
				sideBySideHTML.WriteString("</h2>\n")
				sideBySideHTML.WriteString("    <p>Failed to generate diff output.</p>\n")
			}
		}
	}

	commentBody := comment.String()
	if len(commentBody) > maxCommentChars {
		commentBody = commentBody[:maxCommentChars] + "\n...comment truncated due to size limit...\n"
	}
	if err := os.WriteFile(commentFile, []byte(commentBody), 0o644); err != nil {
		return fmt.Errorf("write comment file: %w", err)
	}
	if resolvedDiffFormat == diffFormatSideBySide {
		sideBySideHTML.WriteString("  </body>\n</html>\n")
		if err := os.WriteFile(htmlFile, []byte(sideBySideHTML.String()), 0o644); err != nil {
			return fmt.Errorf("write html file: %w", err)
		}
	}

	return appendOutputs(outputFile, []outputKV{
		{Key: "has_flow_changes", Value: "true"},
		{Key: "comment_file", Value: commentFile},
		{Key: "html_file", Value: htmlFileOutput},
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

func renderFlow(checkoutDir, flow2apexBin, flowPath, outputDir string) (int, []byte, error) {
	flowFilePath := filepath.Join(checkoutDir, filepath.FromSlash(flowPath))
	if _, err := os.Stat(flowFilePath); err != nil {
		if os.IsNotExist(err) {
			return 2, nil, nil
		}
		return 1, nil, fmt.Errorf("stat flow file %s: %w", flowPath, err)
	}

	var log bytes.Buffer
	ok, stderr, err := runFlow2ApexToDir(checkoutDir, flow2apexBin, flowFilePath, outputDir)
	if err != nil {
		return 1, nil, err
	}
	log.Write(stderr)
	if ok {
		return 0, log.Bytes(), nil
	}

	ok, stdout, stderr, err := runFlow2ApexToStdout(checkoutDir, flow2apexBin, flowFilePath)
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

func runFlow2ApexToDir(checkoutDir, bin, flowFile, outputDir string) (bool, []byte, error) {
	cmd := exec.Command(bin, flowFile, "-d", outputDir)
	cmd.Dir = checkoutDir
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

func runFlow2ApexToStdout(checkoutDir, bin, flowFile string) (bool, []byte, []byte, error) {
	cmd := exec.Command(bin, flowFile)
	cmd.Dir = checkoutDir
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

func createDetachedWorktree(workspace, sha, dir string) error {
	cmd := exec.Command("git", "worktree", "add", "--detach", dir, sha)
	cmd.Dir = workspace
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("create worktree for %s: %s", sha, msg)
		}
		return fmt.Errorf("create worktree for %s: %w", sha, err)
	}
	return nil
}

func removeWorktree(workspace, dir string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", dir)
	cmd.Dir = workspace
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			return nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("remove worktree %s: %s", dir, msg)
		}
		return fmt.Errorf("remove worktree %s: %w", dir, err)
	}
	return nil
}

func diffRenderedOutputs(workspace, flowPath, baseDir, headDir, diffFormat string) (int, string, error) {
	switch diffFormat {
	case diffFormatSideBySide:
		diffExit, diffText, err := diffSideBySide(workspace, flowPath, baseDir, headDir)
		if err != nil {
			return 2, "", err
		}
		return diffExit, diffText, nil
	default:
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
		diffExit, diffText, _, err := runDiffCommand(cmd)
		if err != nil {
			return 2, "", fmt.Errorf("generate diff output: %w", err)
		}
		return diffExit, diffText, nil
	}
}

func normalizeDiffFormat(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", diffFormatUnified:
		return diffFormatUnified, nil
	case diffFormatSideBySide:
		return diffFormatSideBySide, nil
	default:
		return "", fmt.Errorf("invalid diff-format %q (expected %q or %q)", value, diffFormatUnified, diffFormatSideBySide)
	}
}

func diffCommentMarker(diffFormat string) string {
	return fmt.Sprintf("<!-- flow2apex-diff-comment:%s -->", diffFormat)
}

func startSideBySideHTMLReport(baseSHA, headSHA string) string {
	return "<!doctype html>\n<html lang=\"en\">\n" +
		"  <head>\n" +
		"    <meta charset=\"utf-8\" />\n" +
		"    <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\" />\n" +
		"    <title>flow2apex Side-By-Side Diff</title>\n" +
		"    <style>\n" +
		"      :root { color-scheme: light; }\n" +
		"      body { margin: 24px; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, \"Liberation Mono\", \"Courier New\", monospace; color: #1f2328; background: #ffffff; }\n" +
		"      h1 { margin: 0 0 12px 0; font-size: 22px; }\n" +
		"      h2 { margin: 24px 0 8px 0; font-size: 16px; }\n" +
		"      p { margin: 0 0 12px 0; font-size: 13px; }\n" +
		"      code { background: #f6f8fa; border-radius: 4px; padding: 1px 4px; }\n" +
		"      pre.sbs { margin: 0 0 16px 0; padding: 12px; overflow-x: auto; overflow-y: hidden; border: 1px solid #d0d7de; border-radius: 6px; background: #f6f8fa; line-height: 1.35; }\n" +
		"      .sbs-scale { display: block; width: max-content; min-width: 100%; transform-origin: left top; }\n" +
		"      .left { color: #cf222e; }\n" +
		"      .right { color: #1a7f37; }\n" +
		"      .sep { color: #656d76; }\n" +
		"    </style>\n" +
		"    <script>\n" +
		"      function fitSideBySideDiffs() {\n" +
		"        const blocks = document.querySelectorAll('pre.sbs');\n" +
		"        for (const pre of blocks) {\n" +
		"          const scaleNode = pre.querySelector('.sbs-scale');\n" +
		"          if (!scaleNode) {\n" +
		"            continue;\n" +
		"          }\n" +
		"          scaleNode.style.transform = '';\n" +
		"          pre.style.height = '';\n" +
		"          pre.style.overflowX = 'auto';\n" +
		"          pre.style.overflowY = 'hidden';\n" +
		"          const available = pre.clientWidth;\n" +
		"          const needed = scaleNode.scrollWidth;\n" +
		"          if (!available || !needed || needed <= available) {\n" +
		"            continue;\n" +
		"          }\n" +
		"          const scale = available / needed;\n" +
		"          const minScale = 0.90;\n" +
		"          if (scale < minScale) {\n" +
		"            continue;\n" +
		"          }\n" +
		"          scaleNode.style.transform = 'scale(' + scale + ')';\n" +
		"          pre.style.height = Math.ceil((scaleNode.scrollHeight * scale) + 24) + 'px';\n" +
		"          pre.style.overflowX = 'hidden';\n" +
		"          pre.style.overflowY = 'hidden';\n" +
		"        }\n" +
		"      }\n" +
		"      function scheduleFit() {\n" +
		"        fitSideBySideDiffs();\n" +
		"        window.requestAnimationFrame(fitSideBySideDiffs);\n" +
		"        window.setTimeout(fitSideBySideDiffs, 120);\n" +
		"      }\n" +
		"      window.addEventListener('load', scheduleFit);\n" +
		"      window.addEventListener('resize', fitSideBySideDiffs);\n" +
		"    </script>\n" +
		"  </head>\n" +
		"  <body>\n" +
		"    <h1>flow2apex Side-By-Side Diffs</h1>\n" +
		"    <p>Compared generated Apex between base <code>" + html.EscapeString(baseSHA) + "</code> and head <code>" + html.EscapeString(headSHA) + "</code>.</p>\n"
}

func rewriteSideBySideDiffPaths(diffText, flowPath, baseDir, headDir string) string {
	replacer := strings.NewReplacer(
		baseDir, "a/"+flowPath,
		headDir, "b/"+flowPath,
	)
	return replacer.Replace(diffText)
}

func diffSideBySide(workspace, flowPath, baseDir, headDir string) (int, string, error) {
	type sideBySideAttempt struct {
		expandTabs bool
	}
	attempts := []sideBySideAttempt{
		{expandTabs: true},
		{expandTabs: false},
	}

	for _, attempt := range attempts {
		cmd := buildSideBySideDiffCommand(workspace, baseDir, headDir, attempt.expandTabs)
		diffExit, diffText, stderrText, err := runDiffCommand(cmd)
		if err != nil {
			return 2, "", fmt.Errorf("generate side-by-side diff output: %w", err)
		}

		if diffExit == 2 && sideBySideOptionUnsupported(stderrText) {
			continue
		}

		diffText = rewriteSideBySideDiffPaths(diffText, flowPath, baseDir, headDir)
		diffText = removeSideBySideCommandHeaders(diffText)
		return diffExit, diffText, nil
	}

	return 2, "", fmt.Errorf("generate side-by-side diff output: diff options are not supported")
}

func buildSideBySideDiffCommand(workspace, baseDir, headDir string, expandTabs bool) *exec.Cmd {
	args := []string{
		"--recursive",
		"--side-by-side",
		"--new-file",
		fmt.Sprintf("--width=%d", sideBySideWidth),
		fmt.Sprintf("--tabsize=%d", sideBySideTabSize),
	}
	if expandTabs {
		args = append(args, "--expand-tabs")
	}
	args = append(args, baseDir, headDir)

	cmd := exec.Command("diff", args...)
	cmd.Dir = workspace
	return cmd
}

func runDiffCommand(cmd *exec.Cmd) (int, string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String(), nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), stdout.String(), stderr.String(), nil
	}
	return 2, "", "", err
}

func sideBySideOptionUnsupported(stderrText string) bool {
	lower := strings.ToLower(stderrText)
	return strings.Contains(lower, "unrecognized option") ||
		strings.Contains(lower, "illegal option") ||
		strings.Contains(lower, "unknown option")
}

func removeSideBySideCommandHeaders(diffText string) string {
	if diffText == "" {
		return diffText
	}
	lines := strings.Split(diffText, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --recursive --side-by-side ") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func formatSideBySideDiffHTML(diffText string) string {
	if diffText == "" {
		return ""
	}
	lines := strings.Split(diffText, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, formatSideBySideDiffHTMLLine(line))
	}
	return strings.Join(out, "\n")
}

func formatSideBySideDiffHTMLLine(line string) string {
	if line == "" {
		return ""
	}
	markerIdx, marker, ok := findSideBySideMarker(line)
	if !ok {
		return html.EscapeString(line)
	}

	switch marker {
	case '|':
		left := html.EscapeString(line[:markerIdx])
		right := html.EscapeString(line[markerIdx+1:])
		return "<span class=\"left\">" + left + "</span><span class=\"sep\">|</span><span class=\"right\">" + right + "</span>"
	case '<':
		leftWithMarker := html.EscapeString(line[:markerIdx+1])
		right := html.EscapeString(line[markerIdx+1:])
		return "<span class=\"left\">" + leftWithMarker + "</span>" + right
	case '>':
		left := html.EscapeString(line[:markerIdx])
		rightWithMarker := html.EscapeString(line[markerIdx:])
		return left + "<span class=\"right\">" + rightWithMarker + "</span>"
	default:
		return html.EscapeString(line)
	}
}

func findSideBySideMarker(line string) (int, byte, bool) {
	if len(line) == 0 {
		return 0, 0, false
	}
	mid := (sideBySideWidth / 2) - 1
	if mid < 0 {
		mid = 0
	}
	if mid >= len(line) {
		return 0, 0, false
	}
	marker := line[mid]
	switch marker {
	case '|', '<', '>':
		if isLikelySideBySideMarker(line, mid, marker) {
			return mid, marker, true
		}
	}
	return 0, 0, false
}

func isLikelySideBySideMarker(line string, idx int, marker byte) bool {
	if idx < 0 || idx >= len(line) {
		return false
	}
	if idx == 0 {
		return false
	}
	prev := line[idx-1]
	if prev != ' ' && prev != '\t' {
		return false
	}
	if idx+1 >= len(line) {
		return marker == '<' || marker == '>'
	}
	next := line[idx+1]
	return next == ' ' || next == '\t'
}

func suppressCommonSideBySideDiffLines(diffText string) string {
	if diffText == "" {
		return diffText
	}
	lines := strings.Split(diffText, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if _, _, ok := findSideBySideMarker(line); ok {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
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
