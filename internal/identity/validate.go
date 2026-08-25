package identity

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var skillNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)sk-[A-Za-z0-9]{10,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`-----BEGIN [^-]+ PRIVATE KEY-----`),
}

func SkillPackageName(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		return "", err
	}
	frontmatter, _, ok := splitFrontmatter(string(data))
	if !ok {
		return "", fmt.Errorf("SKILL.md frontmatter is required")
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &fields); err != nil {
		return "", err
	}
	name, _ := fields["name"].(string)
	if !skillNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid skill name")
	}
	return name, nil
}

func ValidateSkillPackage(root string) error {
	if _, err := os.Stat(filepath.Join(root, "SKILL.md")); err != nil {
		return fmt.Errorf("missing root SKILL.md: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		return err
	}
	for _, pattern := range secretPatterns {
		if pattern.Match(data) {
			return fmt.Errorf("secret-like content in SKILL.md")
		}
	}
	if scanErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if path == filepath.Join(root, "SKILL.md") {
			return nil
		}
		bytes, err := os.ReadFile(path)
		if err == nil {
			for _, pattern := range secretPatterns {
				if pattern.Match(bytes) {
					return fmt.Errorf("secret-like content in %s", path)
				}
			}
		}
		return nil
	}); scanErr != nil {
		return scanErr
	}
	frontmatter, body, ok := splitFrontmatter(string(data))
	if !ok {
		return fmt.Errorf("SKILL.md frontmatter is required")
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &fields); err != nil {
		return fmt.Errorf("invalid frontmatter: %w", err)
	}
	name, _ := fields["name"].(string)
	description, _ := fields["description"].(string)
	if !skillNamePattern.MatchString(name) {
		return fmt.Errorf("invalid skill name")
	}
	if strings.TrimSpace(description) == "" {
		return fmt.Errorf("skill description is required")
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("SKILL.md body is required")
	}
	return nil
}

func splitFrontmatter(content string) (string, string, bool) {
	content = strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", content, false
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return "", content, false
	}
	end += 4
	return content[4:end], content[end+4:], true
}
