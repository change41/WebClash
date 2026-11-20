// Package cli ports server/src/cli.ts: argument parsing, command dispatch and
// exit codes. Service commands are wired in G8; run in G2.
package cli

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"webclash/internal/bootstrap"
	"webclash/internal/paths"
	"webclash/internal/service"
	"webclash/internal/util"
)

// CliError carries a process exit code (port of CliError).
type CliError struct {
	ExitCode int
	Message  string
}

func (e *CliError) Error() string { return e.Message }

// FlagSpec describes one CLI flag.
type FlagSpec struct {
	Long  string
	Short string
	Kind  string // "string" | "boolean"
}

var globalFlags = []FlagSpec{
	{Long: "help", Short: "h", Kind: "boolean"},
	{Long: "version", Short: "v", Kind: "boolean"},
	{Long: "json", Kind: "boolean"},
	{Long: "dry-run", Kind: "boolean"},
	{Long: "yes", Short: "y", Kind: "boolean"},
}

var runFlags = []FlagSpec{
	{Long: "port", Short: "p", Kind: "string"},
	{Long: "host", Short: "H", Kind: "string"},
	{Long: "data-dir", Short: "d", Kind: "string"},
	{Long: "password", Kind: "string"},
	{Long: "web-dist", Kind: "string"},
}

var serviceFlags = []FlagSpec{
	{Long: "name", Short: "n", Kind: "string"},
	{Long: "service-manager", Kind: "string"},
	{Long: "data-dir", Short: "d", Kind: "string"},
	{Long: "port", Short: "p", Kind: "string"},
	{Long: "host", Short: "H", Kind: "string"},
	{Long: "bin", Kind: "string"},
}

var installFlags = append(append([]FlagSpec{}, serviceFlags...),
	FlagSpec{Long: "no-copy", Kind: "boolean"},
	FlagSpec{Long: "user", Kind: "string"},
	FlagSpec{Long: "tun", Kind: "boolean"},
	FlagSpec{Long: "force", Kind: "boolean"},
	FlagSpec{Long: "no-start", Kind: "boolean"},
)

var uninstallFlags = append(append([]FlagSpec{}, serviceFlags...),
	FlagSpec{Long: "remove-bin", Kind: "boolean"},
	FlagSpec{Long: "purge", Kind: "boolean"},
)

var commandFlags = map[string][]FlagSpec{
	"run":       runFlags,
	"install":   installFlags,
	"uninstall": uninstallFlags,
	"start":     serviceFlags,
	"stop":      serviceFlags,
	"restart":   serviceFlags,
	"status":    serviceFlags,
	"password":  {{Long: "data-dir", Short: "d", Kind: "string"}},
	"version":   {},
	"help":      {},
}

func allSpecs() []FlagSpec {
	out := append([]FlagSpec{}, globalFlags...)
	for _, specs := range commandFlags {
		out = append(out, specs...)
	}
	return out
}

// ParsedArgs holds flag values (string or bool) and positional args.
type ParsedArgs struct {
	Values     map[string]any
	Positional []string
}

func findSpec(specs []FlagSpec, name string) *FlagSpec {
	for i := range specs {
		if specs[i].Long == name || (specs[i].Short != "" && specs[i].Short == name) {
			return &specs[i]
		}
	}
	return nil
}

// ParseArgs ports cli.ts parseArgs, including --, --k=v, short flags and the
// exact Chinese error messages / exit code 2.
func ParseArgs(argv []string, specs []FlagSpec) (*ParsedArgs, error) {
	values := map[string]any{}
	positional := []string{}
	noMoreFlags := false
	for i := 0; i < len(argv); i++ {
		token := argv[i]
		if noMoreFlags || !strings.HasPrefix(token, "-") || token == "-" {
			positional = append(positional, token)
			continue
		}
		if token == "--" {
			noMoreFlags = true
			continue
		}
		var name string
		var inlineValue *string
		if strings.HasPrefix(token, "--") {
			if eq := strings.IndexByte(token, '='); eq >= 0 {
				name = token[2:eq]
				v := token[eq+1:]
				inlineValue = &v
			} else {
				name = token[2:]
			}
		} else {
			name = token[1:]
		}
		spec := findSpec(specs, name)
		if spec == nil {
			return nil, &CliError{2, fmt.Sprintf("未知参数: %s（使用 --help 查看用法）", token)}
		}
		if spec.Kind == "boolean" {
			if inlineValue != nil && *inlineValue != "true" && *inlineValue != "false" {
				return nil, &CliError{2, fmt.Sprintf("参数 --%s 只接受 true/false", spec.Long)}
			}
			values[spec.Long] = inlineValue == nil || *inlineValue != "false"
			continue
		}
		var value string
		if inlineValue != nil {
			value = *inlineValue
		} else {
			if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "-") {
				return nil, &CliError{2, fmt.Sprintf("参数 --%s 需要一个值", spec.Long)}
			}
			value = argv[i+1]
			i++
		}
		if value == "" {
			return nil, &CliError{2, fmt.Sprintf("参数 --%s 的值不能为空", spec.Long)}
		}
		values[spec.Long] = value
	}
	return &ParsedArgs{Values: values, Positional: positional}, nil
}

func strVal(values map[string]any, key string) string {
	if v, ok := values[key].(string); ok {
		return v
	}
	return ""
}

func boolVal(values map[string]any, key string) bool {
	if v, ok := values[key].(bool); ok {
		return v
	}
	return false
}

func parsePort(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || f < 1 || f > 65535 {
		return 0, &CliError{2, fmt.Sprintf("无效端口: %s", raw)}
	}
	return int(f), nil
}

func absResolve(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

func commandPassword(values map[string]any) int {
	dataDir := paths.DefaultDataDir()
	if d := strVal(values, "data-dir"); d != "" {
		dataDir = absResolve(paths.ExpandHome(d))
	}
	file := filepath.Join(dataDir, "initial-password.txt")
	raw, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "未找到初始密码文件: %s\n", file)
		fmt.Fprintln(os.Stderr, "若已修改密码或文件被删除，可删除 settings.json 中的 auth.passwordHash 后重启以重置。")
		return 1
	}
	password := strings.TrimSpace(string(raw))
	if boolVal(values, "json") {
		b, _ := util.MarshalJSON(map[string]any{"password": password, "file": file})
		fmt.Println(string(b))
	} else {
		fmt.Println(password)
	}
	return 0
}

func applyRunFlags(values map[string]any) error {
	if v := strVal(values, "port"); v != "" {
		n, err := parsePort(v, 7788)
		if err != nil {
			return err
		}
		_ = os.Setenv("WEBUI_PORT", strconv.Itoa(n))
	}
	if v := strVal(values, "host"); v != "" {
		_ = os.Setenv("WEBUI_HOST", v)
	}
	if v := strVal(values, "data-dir"); v != "" {
		_ = os.Setenv("DATA_DIR", absResolve(paths.ExpandHome(v)))
	}
	if v := strVal(values, "password"); v != "" {
		_ = os.Setenv("WEBUI_PASSWORD", v)
	}
	if v := strVal(values, "web-dist"); v != "" {
		_ = os.Setenv("WEBCLASH_WEB_DIST", absResolve(paths.ExpandHome(v)))
	}
	return nil
}

func managerFlag(values map[string]any) (string, error) {
	raw := strVal(values, "service-manager")
	if raw == "" {
		raw = "auto"
	}
	if raw == "auto" {
		return "", nil
	}
	if raw == "systemd" || raw == "openrc" {
		return raw, nil
	}
	return "", &CliError{2, fmt.Sprintf("--service-manager 只接受 auto/systemd/openrc，收到: %s", raw)}
}

func resolveFromFlags(values map[string]any) (service.ServiceOptions, error) {
	port, err := parsePort(strVal(values, "port"), 7788)
	if err != nil {
		return service.ServiceOptions{}, err
	}
	manager, err := managerFlag(values)
	if err != nil {
		return service.ServiceOptions{}, err
	}
	return service.ResolveService(service.ServiceInput{
		Name:    strVal(values, "name"),
		Bin:     strVal(values, "bin"),
		DataDir: strVal(values, "data-dir"),
		Port:    port,
		Host:    strVal(values, "host"),
		User:    strVal(values, "user"),
		Tun:     boolVal(values, "tun"),
		Manager: manager,
	})
}

func requireManager(o service.ServiceOptions) error {
	if o.Manager == "none" {
		return &CliError{4, "未检测到 systemd 或 OpenRC；可用 --service-manager 强制指定，或直接 webclash run 前台运行"}
	}
	return nil
}

func requireRoot(dryRun bool) error {
	if !dryRun && !service.IsRoot() {
		return &CliError{5, "该操作需要 root 权限，请使用 sudo"}
	}
	return nil
}

func printSteps(steps []service.StepResult, jsonOut bool) {
	if steps == nil {
		steps = []service.StepResult{}
	}
	if jsonOut {
		b, _ := util.MarshalJSONIndent(steps)
		fmt.Println(string(b))
		return
	}
	for _, s := range steps {
		fmt.Printf("[%s] %s: %s\n", s.Action, s.Step, s.Detail)
	}
}

type statusJSON struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Manager   string `json:"manager"`
	Active    bool   `json:"active"`
	Healthy   bool   `json:"healthy"`
}

func dispatch(command string, values map[string]any) int {
	jsonOut := boolVal(values, "json")
	dryRun := boolVal(values, "dry-run")

	switch command {
	case "help":
		fmt.Println(formatHelp())
		return 0
	case "version":
		printVersion(jsonOut)
		return 0
	case "password":
		return commandPassword(values)
	case "run":
		if err := applyRunFlags(values); err != nil {
			return cliErrExit(err)
		}
		if err := bootstrap.StartServer(); err != nil {
			return cliErrExit(err)
		}
		return 0
	case "install":
		o, err := resolveFromFlags(values)
		if err != nil {
			return cliErrExit(err)
		}
		if err := requireManager(o); err != nil {
			return cliErrExit(err)
		}
		if err := requireRoot(dryRun); err != nil {
			return cliErrExit(err)
		}
		steps, err := service.InstallService(o, service.InstallFlags{
			DryRun:  dryRun,
			Force:   boolVal(values, "force"),
			NoCopy:  boolVal(values, "no-copy"),
			NoStart: boolVal(values, "no-start"),
		})
		if err != nil {
			return cliErrExit(err)
		}
		printSteps(steps, jsonOut)
		if !dryRun {
			fmt.Printf("\n服务已安装：%s（%s）\n", o.Name, o.Manager)
			fmt.Printf("访问: http://<主机地址>:%d\n", o.Port)
			fmt.Printf("管理密码: %s password --data-dir %s\n", o.Bin, o.DataDir)
			logCmd := fmt.Sprintf("tail -f /var/log/%s.log", o.Name)
			if o.Manager == "systemd" {
				logCmd = fmt.Sprintf("journalctl -u %s -f", o.Name)
			}
			fmt.Printf("日志: %s\n", logCmd)
		}
		return 0
	case "uninstall":
		o, err := resolveFromFlags(values)
		if err != nil {
			return cliErrExit(err)
		}
		if err := requireManager(o); err != nil {
			return cliErrExit(err)
		}
		purge := boolVal(values, "purge")
		if purge && !boolVal(values, "yes") {
			return cliErrExit(&CliError{2, fmt.Sprintf("--purge 将永久删除 %s、配置与二进制，需加 --yes 确认", o.DataDir)})
		}
		if err := requireRoot(dryRun); err != nil {
			return cliErrExit(err)
		}
		steps, err := service.UninstallService(o, service.UninstallFlags{
			DryRun:    dryRun,
			RemoveBin: boolVal(values, "remove-bin"),
			Purge:     purge,
		})
		if err != nil {
			return cliErrExit(err)
		}
		printSteps(steps, jsonOut)
		if !dryRun && !purge {
			fmt.Printf("\n数据已保留: %s\n", o.DataDir)
			fmt.Printf("彻底清除: sudo %s uninstall --purge --yes -d %s\n", o.Bin, o.DataDir)
		}
		return 0
	case "start", "stop", "restart":
		o, err := resolveFromFlags(values)
		if err != nil {
			return cliErrExit(err)
		}
		if err := requireManager(o); err != nil {
			return cliErrExit(err)
		}
		if err := requireRoot(dryRun); err != nil {
			return cliErrExit(err)
		}
		steps, err := service.ServiceAction(o, command, dryRun)
		if err != nil {
			return cliErrExit(err)
		}
		printSteps(steps, jsonOut)
		return 0
	case "status":
		o, err := resolveFromFlags(values)
		if err != nil {
			return cliErrExit(err)
		}
		st := service.GetServiceStatus(o)
		if jsonOut {
			b, _ := util.MarshalJSONIndent(statusJSON{
				Name:      o.Name,
				Installed: st.Installed,
				Manager:   st.Manager,
				Active:    st.Active,
				Healthy:   st.Healthy,
			})
			fmt.Println(string(b))
		} else if !st.Installed {
			fmt.Printf("%s: 未安装\n", o.Name)
		} else {
			state := "已停止"
			if st.Active {
				state = "运行中"
			}
			suffix := ""
			if st.Active && !st.Healthy {
				suffix = "，健康检查未通过"
			}
			fmt.Printf("%s: %s（%s）%s\n", o.Name, state, o.Manager, suffix)
		}
		if !st.Installed {
			return 4
		}
		if !st.Active || !st.Healthy {
			return 3
		}
		return 0
	default:
		return cliErrExit(&CliError{2, fmt.Sprintf("未知命令: %s（使用 --help 查看用法）", command)})
	}
}

func cliErrExit(err error) int {
	if ce, ok := err.(*CliError); ok {
		fmt.Fprintln(os.Stderr, ce.Message)
		return ce.ExitCode
	}
	fmt.Fprintf(os.Stderr, "错误：%s\n", util.ErrorMessage(err))
	return 1
}

// Run parses argv and dispatches, returning the process exit code.
func Run(argv []string) int {
	parsed, err := ParseArgs(argv, allSpecs())
	if err != nil {
		if ce, ok := err.(*CliError); ok {
			fmt.Fprintln(os.Stderr, ce.Message)
			return ce.ExitCode
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	values := parsed.Values

	if boolVal(values, "version") {
		printVersion(boolVal(values, "json"))
		return 0
	}

	command := "run"
	if len(parsed.Positional) > 0 {
		command = parsed.Positional[0]
	} else if boolVal(values, "help") {
		command = "help"
	}

	specs, known := commandFlags[command]
	if !known {
		fmt.Fprintf(os.Stderr, "未知命令: %s（使用 --help 查看用法）\n", command)
		return 2
	}
	allowed := map[string]bool{}
	for _, s := range globalFlags {
		allowed[s.Long] = true
	}
	for _, s := range specs {
		allowed[s.Long] = true
	}
	for key := range values {
		if !allowed[key] {
			fmt.Fprintf(os.Stderr, "命令 %s 不支持参数 --%s\n", command, key)
			return 2
		}
	}
	if boolVal(values, "help") {
		fmt.Println(formatHelp())
		return 0
	}

	code := dispatch(command, values)
	return code
}
