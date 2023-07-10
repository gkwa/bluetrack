package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/template"

	"gopkg.in/yaml.v2"
)

const terraformTemplate = `
{{- range $index, $rule := .Rules }}
resource "aws_security_group_rule" "{{ $rule.Name }}" {
  type              = "{{ $rule.Type }}"
  from_port         = {{ $rule.FromPort }}
  to_port           = {{ $rule.ToPort }}
  protocol          = "{{ $rule.Protocol }}"
  security_group_id = data.aws_security_group.northflier.id
  cidr_blocks       = ["{{ join $rule.CIDRBlocks "," }}"]
  description       = "{{ $rule.Description }}"
}
{{ end -}}
`

const lxcTemplate = `#!/usr/bin/env bash
{{ range $index, $rule := .Rules -}}
{{- if excludeICMPRule $rule }}
{{- $cidr := splitAtSlash (index $rule.CIDRBlocks 0) }}
lxc config device add csls {{ $rule.Name }} proxy listen={{ $rule.Protocol }}:{{ $cidr -}}
{{ if eq $rule.FromPort $rule.ToPort }}:{{ $rule.FromPort -}}
{{ else }}:{{ $rule.FromPort }}-{{ $rule.ToPort -}}
{{- end }} connect={{ $rule.Protocol }}:127.0.0.1
{{- if eq $rule.FromPort $rule.ToPort }}:{{ $rule.FromPort }}
{{- else }}:{{ $rule.FromPort }}-{{ $rule.ToPort }}
{{- end }}
{{- end }}
{{- end }}
`

type Rule struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	FromPort    int      `yaml:"from_port"`
	ToPort      int      `yaml:"to_port"`
	Protocol    string   `yaml:"protocol"`
	CIDRBlocks  []string `yaml:"cidr_blocks"`
	Description string   `yaml:"description"`
}

type Config struct {
	Rules []Rule `yaml:"rules"`
}

func splitAtSlash(s string) string {
	parts := strings.Split(s, "/")
	return parts[0]
}

func excludeICMPRule(rule Rule) bool {
	return rule.Protocol != "icmp"
}

func main() {
	var yamlFilePath string
	flag.StringVar(&yamlFilePath, "config", "firewall.yaml", "path to the YAML file")

	var firewallScript string
	flag.StringVar(&firewallScript, "script", "firewall.sh", "path to lxc container network config script")

	flag.Parse()

	// Open the YAML file
	file, err := os.Open(yamlFilePath)
	if err != nil {
		fmt.Println("Failed to open YAML file:", err)
		return
	}
	defer file.Close()

	// Parse YAML data into the configuration struct
	config := Config{}
	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		fmt.Println("Failed to parse YAML:", err)
		return
	}

	// Prepare the templates
	tmpl, err := template.New("terraform").Funcs(template.FuncMap{
		"join":         strings.Join,
		"splitAtSlash": splitAtSlash,
	}).Parse(terraformTemplate)
	if err != nil {
		fmt.Println("Failed to parse template:", err)
		return
	}

	lxcTmpl, err := template.New("lxc").Funcs(template.FuncMap{
		"join":           strings.Join,
		"splitAtSlash":   splitAtSlash,
		"excludeICMPRule": excludeICMPRule,
	}).Parse(lxcTemplate)
	if err != nil {
		fmt.Println("Failed to split:", err)
		return
	}

	// Create and open the output files
	outFile, err := os.Create("sg_rules.tf")
	if err != nil {
		fmt.Println("Failed to create output file:", err)
		return
	}
	defer outFile.Close()

	lxcOutFile, err := os.Create(firewallScript)
	if err != nil {
		fmt.Println("Failed to create output file:", err)
		return
	}
	defer lxcOutFile.Close()

	// Change the permissions of the file to be executable
	err = os.Chmod(firewallScript, 0755)
	if err != nil {
		fmt.Printf("Failed to set execute bit on %s: %s\n", firewallScript, err)
	}

	// Execute the templates and write the results to respective files
	err = tmpl.Execute(outFile, config)
	if err != nil {
		fmt.Println("Failed to execute template:", err)
		return
	}

	err = lxcTmpl.Execute(lxcOutFile, config)
	if err != nil {
		fmt.Println("Failed to execute LXC template:", err)
		return
	}
}
