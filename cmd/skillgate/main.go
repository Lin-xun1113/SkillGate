package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lin-xun1113/SkillGate/internal/identity"
	"github.com/Lin-xun1113/SkillGate/internal/manifest"
	"github.com/Lin-xun1113/SkillGate/internal/registry"
	"github.com/Lin-xun1113/SkillGate/internal/validation"
)

const cliVersion = "skillgate.cli.v1"

func main() {
	jsonOutput := contains(os.Args[1:], "--json")
	if len(os.Args) < 2 {
		os.Exit(returnWithError(jsonOutput, "INVALID_ARGUMENT", "", "命令不能为空。"))
	}
	var code int
	switch os.Args[1] {
	case "skill":
		code = skillCommand(os.Args[2:])
	case "suite":
		code = suiteCommand(os.Args[2:])
	case "compile":
		code = compileCommand(os.Args[2:])
	case "db":
		code = databaseCommand(os.Args[2:])
	case "experiment":
		code = experimentCommand(os.Args[2:])
	case "trial":
		code = trialCommand(os.Args[2:])
	case "scheduler":
		code = schedulerCommand(os.Args[2:])
	case "serve":
		code = serveCommand(os.Args[2:])
	case "ui":
		code = uiCommand(os.Args[2:])
	case "--help", "-h":
		usage()
	default:
		code = returnWithError(jsonOutput, "INVALID_ARGUMENT", os.Args[1], "未知命令。")
	}
	if code != 0 {
		os.Exit(code)
	}
}

func usage() {
	fmt.Println("skillgate skill validate|register <path> [--json]")
	fmt.Println("skillgate suite validate|register <path> [--registry-root <path>] [--json]")
	fmt.Println("skillgate compile <manifest> [--registry-root <path>] [--json]")
	fmt.Println("skillgate db migrate|status [--database-url <url>] [--json]")
	fmt.Println("skillgate experiment materialize <manifest>|cancel [options] [--json]")
	fmt.Println("skillgate trial claim|start|heartbeat|complete [options] [--request-hash <hash>] [--idempotency-key <key>] [--json]")
	fmt.Println("skillgate scheduler sweep [--limit <n>] [--json]")
	fmt.Println("skillgate serve [--grpc-addr <addr>] [--database-url <url>] [--json]")
	fmt.Println("skillgate ui [--listen <addr>] [--database-url <url>] [--json]")
}

func skillCommand(args []string) int {
	if len(args) < 2 {
		return returnWithError(contains(args, "--json"), "INVALID_ARGUMENT", "skill", "skill 子命令和路径不能为空")
	}
	jsonOutput, action, root := contains(args, "--json"), args[0], args[1]
	if action != "validate" && action != "register" {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", action, "未知 skill 子命令")
	}
	if info, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return returnWithError(jsonOutput, "FILE_NOT_FOUND", root, err.Error())
		}
		return returnWithError(jsonOutput, "IO_ERROR", root, err.Error())
	} else if !info.IsDir() {
		return returnWithError(jsonOutput, "INVALID_SKILL_MANIFEST", root, "Skill 路径必须是目录")
	}
	hash, err := identity.SkillPackageHash(root)
	if err != nil {
		return returnWithError(jsonOutput, "INVALID_SKILL_MANIFEST", root, err.Error())
	}
	if action == "validate" {
		return printSuccess(jsonOutput, map[string]any{"valid": true, "hash": hash})
	}
	store, err := registry.NewFileStore(flagValue(args, "--registry-root", ".skillgate/registry"))
	if err != nil {
		return returnWithError(jsonOutput, "IO_ERROR", "registry", err.Error())
	}
	version, err := store.RegisterSkill(filepath.Base(root), root)
	if err != nil {
		return returnRegistryError(jsonOutput, root, err)
	}
	return printSuccess(jsonOutput, map[string]any{"logical_name": version.Name, "version_hash": version.Hash, "canonical_size_bytes": len(version.Canonical), "registry_path": filepath.Join(flagValue(args, "--registry-root", ".skillgate/registry"), "skills", version.Name, version.Hash), "idempotent": version.Idempotent})
}

func suiteCommand(args []string) int {
	if len(args) < 2 {
		return returnWithError(contains(args, "--json"), "INVALID_ARGUMENT", "suite", "suite 子命令和路径不能为空")
	}
	jsonOutput, action, path := contains(args, "--json"), args[0], args[1]
	if action != "validate" && action != "register" {
		return returnWithError(jsonOutput, "INVALID_ARGUMENT", action, "未知 suite 子命令")
	}
	root, err := os.Getwd()
	if err != nil {
		return returnWithError(jsonOutput, "IO_ERROR", "", err.Error())
	}
	result, diagnostics := validation.ValidateSuite(path, root)
	if len(diagnostics) > 0 {
		return returnDiagnostics(jsonOutput, diagnostics)
	}
	if action == "validate" {
		return printSuccess(jsonOutput, map[string]any{"valid": true, "hash": result.Hash})
	}
	store, err := registry.NewFileStore(flagValue(args, "--registry-root", ".skillgate/registry"))
	if err != nil {
		return returnWithError(jsonOutput, "IO_ERROR", "registry", err.Error())
	}
	version, err := store.RegisterSuite(result.Name, path)
	if err != nil {
		return returnRegistryError(jsonOutput, path, err)
	}
	return printSuccess(jsonOutput, map[string]any{"logical_name": version.Name, "version_hash": version.Hash, "canonical_size_bytes": len(version.Canonical), "registry_path": filepath.Join(flagValue(args, "--registry-root", ".skillgate/registry"), "suites", version.Name, version.Hash), "idempotent": version.Idempotent})
}

func compileCommand(args []string) int {
	if len(args) < 1 || strings.HasPrefix(args[0], "--") {
		return returnWithError(contains(args, "--json"), "INVALID_ARGUMENT", "compile", "Manifest 路径不能为空")
	}
	jsonOutput := contains(args, "--json")
	root, err := os.Getwd()
	if err != nil {
		return returnWithError(jsonOutput, "IO_ERROR", "", err.Error())
	}
	compiled, diagnostics := manifest.Compile(args[0], root)
	if len(diagnostics) > 0 {
		return returnDiagnostics(jsonOutput, diagnostics)
	}
	if jsonOutput {
		return printSuccess(true, map[string]any{"compiled": true, "result": compiled})
	}
	fmt.Printf("compiled=true pairs=%d trials=%d manifest_hash=%s\n", compiled.PairCount, compiled.TrialCount, compiled.ManifestHash)
	return 0
}

func printSuccess(jsonOutput bool, result map[string]any) int {
	if jsonOutput {
		result["version"] = cliVersion
		result["ok"] = true
		data, _ := json.Marshal(result)
		fmt.Println(string(data))
		return 0
	}
	if valid, ok := result["valid"]; ok {
		fmt.Printf("valid=%v hash=%v\n", valid, result["hash"])
		return 0
	}
	fmt.Printf("registered logical_name=%v version_hash=%v idempotent=%v\n", result["logical_name"], result["version_hash"], result["idempotent"])
	return 0
}

func returnDiagnostics(jsonOutput bool, diagnostics []validation.Diagnostic) int {
	if len(diagnostics) == 0 {
		return 0
	}
	code := diagnostics[0].ExitCode()
	if jsonOutput {
		data, _ := json.Marshal(map[string]any{"version": cliVersion, "ok": false, "diagnostics": diagnostics})
		fmt.Println(string(data))
		return code
	}
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(os.Stderr, "%s: %s\n", diagnostic.Code, diagnostic.Message)
	}
	return code
}

func returnWithError(jsonOutput bool, code, path, message string) int {
	diagnostic := map[string]any{"code": code, "severity": "error", "path": path, "message": message}
	exitCode := codeFor(code)
	if jsonOutput {
		data, _ := json.Marshal(map[string]any{"version": cliVersion, "ok": false, "diagnostics": []any{diagnostic}})
		fmt.Println(string(data))
	} else {
		fmt.Fprintf(os.Stderr, "%s: %s\n", code, message)
	}
	return exitCode
}

func returnRegistryError(jsonOutput bool, path string, err error) int {
	code := registry.DiagnosticCode(err)
	if code == "" {
		if strings.Contains(strings.ToLower(err.Error()), "conflict") {
			code = "REGISTRY_CONFLICT"
		} else if os.IsNotExist(err) {
			code = "FILE_NOT_FOUND"
		} else {
			code = "IO_ERROR"
		}
	}
	return returnWithError(jsonOutput, code, path, err.Error())
}

func codeFor(code string) int {
	switch code {
	case "INVALID_ARGUMENT":
		return 2
	case "FILE_NOT_FOUND":
		return 3
	case "PATH_OUTSIDE_ROOT", "SECRET_FIELD_PRESENT":
		return 4
	case "INVALID_SKILL_MANIFEST", "DUPLICATE_CASE_ID", "DUPLICATE_ARM", "MISSING_REQUIRED_ARM", "INVALID_EVALUATION_MODE", "UNSUPPORTED_TREATMENT":
		return 5
	case "PAIR_IDENTITY_MISMATCH", "LEAKAGE_DETECTED", "CONTENT_HASH_MISMATCH":
		return 6
	case "REGISTRY_CONFLICT":
		return 7
	case "IO_ERROR", "DATABASE_UNAVAILABLE", "MIGRATION_FAILED":
		return 8
	case "IDENTITY_CONFLICT", "OWNER_MISMATCH", "LEASE_MISMATCH", "LEASE_EXPIRED", "STATUS_CONFLICT", "RESULT_CONFLICT", "LOGICAL_TRIAL_TERMINAL", "EXPERIMENT_CANCEL_REQUESTED", "RETRY_EXHAUSTED", "NOT_CLAIMABLE", "TRIAL_NOT_FOUND":
		return 9
	default:
		return 1
	}
}
func contains(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}
func flagValue(args []string, name, fallback string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return fallback
}
