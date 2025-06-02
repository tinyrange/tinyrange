package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"

	"slices"

	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/path"
	"gopkg.in/yaml.v3"
)

const (
	CONFIG_VERSION = 2

	PLAN_DIRECTIVE_VERSION = 1
)

type stageInfo struct {
	files map[string]string // files declared in this stage
}

func (s *stageInfo) declareFile(name, filename string) error {
	if name == "" {
		return fmt.Errorf("file name cannot be empty")
	}

	if _, exists := s.files[name]; exists {
		return fmt.Errorf("file %s already declared in stage", name)
	}

	s.files[name] = filename
	return nil
}

func (s *stageInfo) getFile(name string) (any, error) {
	if name == "" {
		return nil, fmt.Errorf("file name cannot be empty")
	}

	if contents, exists := s.files[name]; exists {
		return contents, nil
	}

	return nil, fmt.Errorf("file %s not declared in stage", name)
}

type evaluationContext struct {
	parent       *evaluationContext    // parent context for nested evaluations
	full         bool                  // whether to perform full validation
	files        map[string]string     // declared files in the current context
	stages       map[string]*stageInfo // stages declared in the current context
	currentStage *stageInfo            // current stage being evaluated
}

func (ctx *evaluationContext) NeedsFullValidation() bool {
	if ctx.full {
		return true
	}
	if ctx.parent != nil {
		return ctx.parent.NeedsFullValidation()
	}
	return false
}

func (ctx *evaluationContext) getCurrentStage() *stageInfo {
	if ctx.currentStage != nil {
		return ctx.currentStage
	}
	if ctx.parent != nil {
		return ctx.parent.getCurrentStage()
	}
	return nil
}

func (ctx *evaluationContext) getStage(name string) (*stageInfo, error) {
	if name == "" {
		return nil, fmt.Errorf("stage name cannot be empty")
	}

	if stage, exists := ctx.stages[name]; exists {
		return stage, nil
	}

	if ctx.parent != nil {
		return ctx.parent.getStage(name)
	}

	return nil, fmt.Errorf("stage %s not declared", name)
}

func (ctx *evaluationContext) declareFile(name, contents string) error {
	if name == "" {
		return fmt.Errorf("file name cannot be empty")
	}

	if _, exists := ctx.files[name]; exists {
		return fmt.Errorf("file %s already declared", name)
	}

	ctx.files[name] = name
	return nil
}

func (ctx *evaluationContext) getFile(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("file name cannot be empty")
	}

	if contents, exists := ctx.files[name]; exists {
		return contents, nil
	}

	if ctx.parent != nil {
		return ctx.parent.getFile(name)
	}

	return "", fmt.Errorf("file %s not declared", name)
}

func (ctx *evaluationContext) getFileDirective(name string) (any, error) {
	if name == "" {
		return nil, fmt.Errorf("file directive name cannot be empty")
	}

	if strings.Contains(name, ":") {
		// <stage>:<file>
		parts := strings.SplitN(name, ":", 2)
		stageName := parts[0]
		fileName := parts[1]

		stage, err := ctx.getStage(stageName)
		if err != nil {
			return nil, fmt.Errorf("failed to get stage %s: %w", stageName, err)
		}

		return stage.getFile(fileName)
	} else {
		// global file
		if contents, err := ctx.getFile(name); err != nil {
			return nil, fmt.Errorf("failed to get global file %s: %w", name, err)
		} else {
			return contents, nil
		}
	}
}

func (ctx *evaluationContext) evaluateTemplate(tpl string) (string, error) {
	t := template.New("str")

	t.Funcs(template.FuncMap{
		"get_file": func(name string) (string, error) {
			return ctx.getFile(name)
		},
	})

	t, err := t.Parse(tpl)
	if err != nil {
		return "", fmt.Errorf("failed to parse template %q: %w", tpl, err)
	}

	var sb strings.Builder
	if err := t.Execute(&sb, ctx); err != nil {
		return "", fmt.Errorf("failed to execute template %q: %w", tpl, err)
	}
	return sb.String(), nil
}

func newEvaluationContext(parent *evaluationContext) *evaluationContext {
	return &evaluationContext{
		parent: parent,
		files:  make(map[string]string),
		stages: make(map[string]*stageInfo),
	}
}

type ByteQuantity string

func (b *ByteQuantity) Parse() (uint64, error) {
	if b == nil {
		return 0, fmt.Errorf("byte quantity cannot be nil")
	}

	if *b == "" {
		return 0, fmt.Errorf("byte quantity cannot be empty")
	}

	str := strings.ToUpper(strings.TrimSpace(string(*b)))
	var multiplier uint64 = 1

	if strings.HasSuffix(str, "K") {
		multiplier = 1024
		str = strings.TrimSuffix(str, "K")
	} else if strings.HasSuffix(str, "M") {
		multiplier = 1024 * 1024
		str = strings.TrimSuffix(str, "M")
	} else if strings.HasSuffix(str, "G") {
		multiplier = 1024 * 1024 * 1024
		str = strings.TrimSuffix(str, "G")
	}

	value, err := strconv.ParseUint(str, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid byte quantity %s: %w", *b, err)
	}

	return value * multiplier, nil
}

func (b *ByteQuantity) validate(ctx *evaluationContext) error {
	if b == nil {
		return fmt.Errorf("byte quantity cannot be nil")
	}

	if *b == "" {
		return fmt.Errorf("byte quantity cannot be empty")
	}

	if _, err := b.Parse(); err != nil {
		return fmt.Errorf("invalid byte quantity %s: %w", *b, err)
	}

	return nil
}

type DriverSpecification struct {
	CPUCore int          `yaml:"cpu_core,omitempty"`
	Memory  ByteQuantity `yaml:"memory,omitempty"`  // e.g., "2G", "512M"
	Storage ByteQuantity `yaml:"storage,omitempty"` // e.g., "10G", "5G"
}

func (d *DriverSpecification) validate(ctx *evaluationContext) error {
	if d == nil {
		return fmt.Errorf("driver specification cannot be nil")
	}

	if d.CPUCore < 0 {
		return fmt.Errorf("CPU core count cannot be negative: %d", d.CPUCore)
	}

	value, err := d.Memory.Parse()
	if err != nil {
		return fmt.Errorf("invalid memory specification: %w", err)
	}
	if ctx.NeedsFullValidation() && value < 128*1024*1024 { // minimum 128MB
		return fmt.Errorf("memory must be at least 128MB, got %s", d.Memory)
	}

	if err := d.Storage.validate(ctx); err != nil {
		return fmt.Errorf("invalid storage specification: %w", err)
	}

	return nil
}

type DriverDirective struct {
	MinSpec *DriverSpecification `yaml:"min_spec,omitempty"`
}

func (d *DriverDirective) validate(ctx *evaluationContext) error {
	if d.MinSpec != nil {
		if err := d.MinSpec.validate(ctx); err != nil {
			return fmt.Errorf("driver directive validation failed: %w", err)
		}
	}

	return nil
}

type PlanDirective struct {
	Version  int      `yaml:"version"`
	Builder  *string  `yaml:"builder,omitempty"`
	Packages []string `yaml:"packages,omitempty"`
}

func (p *PlanDirective) validate(ctx *evaluationContext) error {
	if p.Version != PLAN_DIRECTIVE_VERSION {
		return fmt.Errorf("unsupported plan directive version %d, expected %d", p.Version, PLAN_DIRECTIVE_VERSION)
	}

	return nil
}

type FromDirective string

func (f *FromDirective) validate(ctx *evaluationContext) error {
	if f == nil {
		return fmt.Errorf("from directive cannot be nil")
	}

	if *f == "" {
		return fmt.Errorf("from directive cannot be empty")
	}

	return nil
}

type GroupDirective []Directive

func (g *GroupDirective) validate(ctx *evaluationContext) error {
	if g == nil {
		return fmt.Errorf("group directive cannot be nil")
	}

	child := newEvaluationContext(ctx)

	for i, d := range *g {
		if err := d.validate(child); err != nil {
			return fmt.Errorf("group directive validation failed for directive %d: %w", i, err)
		}
	}

	return nil
}

type LayerDirective []Directive

func (l *LayerDirective) validate(ctx *evaluationContext) error {
	if l == nil {
		return fmt.Errorf("layer directive cannot be nil")
	}

	child := newEvaluationContext(ctx)

	for i, d := range *l {
		if err := d.validate(child); err != nil {
			return fmt.Errorf("layer directive validation failed for directive %d: %w", i, err)
		}
	}

	return nil
}

type StageDirective struct {
	Name       string      `yaml:"name"`
	Directives []Directive `yaml:"directives,omitempty"`
}

func (s *StageDirective) validate(ctx *evaluationContext) error {
	if s.Name == "" {
		return fmt.Errorf("stage directive name cannot be empty")
	}

	if s.Directives == nil {
		return fmt.Errorf("stage directive must contain directives")
	}

	stage := &stageInfo{
		files: make(map[string]string),
	}

	child := newEvaluationContext(ctx)
	child.currentStage = stage

	for i, d := range s.Directives {
		if err := d.validate(child); err != nil {
			return fmt.Errorf("stage directive validation failed for directive %d: %w", i, err)
		}
	}

	if ctx.NeedsFullValidation() {
		ctx.stages[s.Name] = stage
	}

	return nil
}

type EnvironmentDirective map[string]string

func (e *EnvironmentDirective) validate(ctx *evaluationContext) error {
	if e == nil {
		return fmt.Errorf("environment directive cannot be nil")
	}

	for key := range *e {
		if key == "" {
			return fmt.Errorf("environment variable key cannot be empty")
		}
	}

	return nil
}

type TemplateString string

func (t *TemplateString) validate(ctx *evaluationContext) error {
	if _, err := ctx.evaluateTemplate(string(*t)); err != nil {
		return fmt.Errorf("invalid template string %q: %w", *t, err)
	}

	return nil
}

type RunDirective []TemplateString

func (r *RunDirective) validate(ctx *evaluationContext) error {
	if r == nil {
		return fmt.Errorf("run directive cannot be nil")
	}

	if len(*r) == 0 {
		return fmt.Errorf("run directive must specify at least one command")
	}

	for i, cmd := range *r {
		if err := cmd.validate(ctx); err != nil {
			return fmt.Errorf("run directive validation failed for command %d: %w", i, err)
		}
	}

	return nil
}

type WorkingDirectoryDirective string

func (w *WorkingDirectoryDirective) validate(ctx *evaluationContext) error {
	if w == nil {
		return fmt.Errorf("working directory directive cannot be nil")
	}

	if *w == "" {
		return fmt.Errorf("working directory cannot be empty")
	}

	return nil
}

type FileDirective struct {
	Name     string `yaml:"name"`
	Contents string `yaml:"contents"`
}

func (f *FileDirective) validate(ctx *evaluationContext) error {
	if f.Name == "" {
		return fmt.Errorf("file directive name cannot be empty")
	}

	if ctx.NeedsFullValidation() {
		if err := ctx.declareFile(f.Name, f.Contents); err != nil {
			return fmt.Errorf("file directive validation failed for file %s: %w", f.Name, err)
		}
	}

	return nil
}

type CopyDirective string

func (c *CopyDirective) validate(ctx *evaluationContext) error {
	if c == nil {
		return fmt.Errorf("copy directive cannot be nil")
	}

	values := strings.Split(string(*c), " ")
	if len(values) < 2 {
		return fmt.Errorf("copy directive must specify at least source and destination, got %d values", len(values))
	}

	if slices.Contains(values, "") {
		return fmt.Errorf("copy directive values cannot be empty")
	}

	if ctx.NeedsFullValidation() {
		source := values[0]
		if _, err := ctx.getFileDirective(source); err != nil {
			return fmt.Errorf("copy directive validation failed for source %s: %w", source, err)
		}
	}

	return nil
}

type OutputDirective []string

func (o *OutputDirective) validate(ctx *evaluationContext) error {
	if o == nil {
		return fmt.Errorf("output directive cannot be nil")
	}

	if len(*o) == 0 {
		return fmt.Errorf("output directive must specify at least one output")
	}

	if slices.Contains(*o, "") {
		return fmt.Errorf("output directive values cannot be empty")
	}

	if ctx.NeedsFullValidation() {
		currentStage := ctx.getCurrentStage()

		if currentStage != nil {
			for _, output := range *o {
				if err := currentStage.declareFile(path.Unix.Base(output), output); err != nil {
					return fmt.Errorf("output directive validation failed for output %s: %w", output, err)
				}
			}
		}
	}

	return nil
}

type EntrypointDirective []string

func (e *EntrypointDirective) validate(ctx *evaluationContext) error {
	if e == nil {
		return fmt.Errorf("entrypoint directive cannot be nil")
	}

	if len(*e) == 0 {
		return fmt.Errorf("entrypoint directive must specify at least one command")
	}

	if slices.Contains(*e, "") {
		return fmt.Errorf("entrypoint directive commands cannot be empty")
	}

	return nil
}

type Directive struct {
	Driver      *DriverDirective           `yaml:"driver,omitempty"`
	Plan        *PlanDirective             `yaml:"plan,omitempty"`
	From        *FromDirective             `yaml:"from,omitempty"`
	Group       *GroupDirective            `yaml:"group,omitempty"`
	Layer       *LayerDirective            `yaml:"layer,omitempty"`
	Stage       *StageDirective            `yaml:"stage,omitempty"`
	Environment *EnvironmentDirective      `yaml:"environment,omitempty"`
	Run         *RunDirective              `yaml:"run,omitempty"`
	WorkingDir  *WorkingDirectoryDirective `yaml:"workdir,omitempty"`
	File        *FileDirective             `yaml:"file,omitempty"`
	Copy        *CopyDirective             `yaml:"copy,omitempty"`
	Output      *OutputDirective           `yaml:"output,omitempty"`
	Entrypoint  *EntrypointDirective       `yaml:"entrypoint,omitempty"`
}

func (d *Directive) validate(ctx *evaluationContext) error {
	if d.Driver != nil {
		return d.Driver.validate(ctx)
	} else if d.Plan != nil {
		return d.Plan.validate(ctx)
	} else if d.From != nil {
		return d.From.validate(ctx)
	} else if d.Group != nil {
		return d.Group.validate(ctx)
	} else if d.Layer != nil {
		return d.Layer.validate(ctx)
	} else if d.Stage != nil {
		return d.Stage.validate(ctx)
	} else if d.Environment != nil {
		return d.Environment.validate(ctx)
	} else if d.Run != nil {
		return d.Run.validate(ctx)
	} else if d.WorkingDir != nil {
		return d.WorkingDir.validate(ctx)
	} else if d.File != nil {
		return d.File.validate(ctx)
	} else if d.Copy != nil {
		return d.Copy.validate(ctx)
	} else if d.Output != nil {
		return d.Output.validate(ctx)
	} else if d.Entrypoint != nil {
		return d.Entrypoint.validate(ctx)
	} else {
		return fmt.Errorf("directive must contain a value")
	}
}

type Config struct {
	Version int `yaml:"version"`

	Directives []Directive `yaml:"directives"`
}

func (c *Config) Validate(full bool) error {
	if c.Version != CONFIG_VERSION {
		return fmt.Errorf("unsupported config version %d, expected %d", c.Version, CONFIG_VERSION)
	}

	ctx := newEvaluationContext(nil)
	ctx.full = full

	for i, d := range c.Directives {
		if err := d.validate(ctx); err != nil {
			return fmt.Errorf("directive %d validation failed: %w", i, err)
		}
	}

	return nil
}

var (
	inputFile = flag.String("input", "experimental/login2/examples/hello.yaml", "Path to the input file")
)

func appMain() error {
	flag.Parse()

	in, err := os.Open(*inputFile)
	if err != nil {
		return fmt.Errorf("failed to open input file %s: %w", *inputFile, err)
	}
	defer in.Close()

	dec := yaml.NewDecoder(in)

	var config Config
	if err := dec.Decode(&config); err != nil {
		return fmt.Errorf("failed to decode YAML: %w", err)
	}

	if err := config.Validate(true); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}
