package tools

import (
	"regexp"
	"strings"
)

type RiskLevel string

const (
	RiskSafe        RiskLevel = "safe"
	RiskModerate    RiskLevel = "moderate"
	RiskDestructive RiskLevel = "destructive"
)

type RiskAssessment struct {
	Level   RiskLevel
	Reasons []string
}

var destructivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\brm\s+-rf\b`),
	regexp.MustCompile(`\bmkfs(\.| )`),
	regexp.MustCompile(`\bdd\s+if=`),
	regexp.MustCompile(`\bshutdown\b`),
	regexp.MustCompile(`\breboot\b`),
	regexp.MustCompile(`\buserdel\b`),
	regexp.MustCompile(`\bchown\b.+\s+/`),
	regexp.MustCompile(`\bclawgo\s+uninstall\b`),
	regexp.MustCompile(`\bdbt\s+drop\b`),
	regexp.MustCompile(`\bgit\s+clean\b`),
}

var moderatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bgit\s+reset\s+--hard\b`),
	regexp.MustCompile(`\bdocker\s+system\s+prune\b`),
	regexp.MustCompile(`\bapt(-get)?\s+install\b`),
	regexp.MustCompile(`\byum\s+install\b`),
	regexp.MustCompile(`\bpip\s+install\b`),
}

func assessCommandRisk(command string) RiskAssessment {
	cmd := strings.ToLower(strings.TrimSpace(command))
	out := RiskAssessment{Level: RiskSafe, Reasons: []string{}}

	for _, re := range destructivePatterns {
		if re.MatchString(cmd) {
			out.Level = RiskDestructive
			out.Reasons = append(out.Reasons, "destructive pattern: "+re.String())
		}
	}
	if out.Level == RiskDestructive {
		return out
	}

	for _, re := range moderatePatterns {
		if re.MatchString(cmd) {
			out.Level = RiskModerate
			out.Reasons = append(out.Reasons, "moderate pattern: "+re.String())
		}
	}
	return out
}

func buildDryRunCommand(command string) (string, bool) {
	trimmed := strings.TrimSpace(command)
	lower := strings.ToLower(trimmed)

	switch {
	case strings.HasPrefix(lower, "apt ") || strings.HasPrefix(lower, "apt-get "):
		if strings.Contains(lower, "--dry-run") || strings.Contains(lower, "-s ") {
			return trimmed, true
		}
		return trimmed + " --dry-run", true
	case strings.HasPrefix(lower, "yum "):
		if strings.Contains(lower, "--assumeno") {
			return trimmed, true
		}
		return trimmed + " --assumeno", true
	case strings.HasPrefix(lower, "dnf "):
		if strings.Contains(lower, "--assumeno") {
			return trimmed, true
		}
		return trimmed + " --assumeno", true
	case strings.HasPrefix(lower, "git clean"):
		if strings.Contains(lower, "-n") || strings.Contains(lower, "--dry-run") {
			return trimmed, true
		}
		return trimmed + " -n", true
	default:
		return "", false
	}
}
