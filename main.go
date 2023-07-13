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
  security_group_id = data.aws_security_group.{{ $.SecurityGroupName }}.id
  cidr_blocks       = ["{{ join $rule.CIDRBlocks "," }}"]
  description       = "{{ $rule.Description }}"
}
{{ end -}}
`

const lxcTemplate = `#!/usr/bin/env bash
{{ range $index, $rule := .Rules -}}
{{- $cidr := splitAtSlash (index $rule.CIDRBlocks 0) }}
lxc config device add {{ $.LXCName }} {{ $rule.Name }} proxy listen={{ $rule.Protocol }}:{{ $cidr }}{{ $rule.FormattedPortRange }} connect={{ $rule.LXCConnect }}
{{- end }}
`

const lxdTemplate = `
config: {}
description: ""
devices:
{{- range $index, $rule := .Rules }}
  {{ $rule.Name }}:
    connect: {{ $rule.LXCConnect }}
    listen: {{ $rule.Protocol }}:{{ splitAtSlash (index $rule.CIDRBlocks 0) }}{{ $rule.FormattedPortRange }}
    type: proxy
{{ end -}}
name: {{ $.LXCName }}
`

type Rule struct {
	Name               string   `yaml:"name"`
	Type               string   `yaml:"type"`
	FromPort           int      `yaml:"from_port"`
	ToPort             int      `yaml:"to_port"`
	Protocol           string   `yaml:"protocol"`
	CIDRBlocks         []string `yaml:"cidr_blocks"`
	Description        string   `yaml:"description"`
	LXCForward         int      `yaml:"lxc_forward"`
	FormattedPortRange string   `yaml:"-"`
	LXCConnect         string   `yaml:"-"`
}

type Config struct {
	Rules             []Rule `yaml:"rules"`
	LXCName           string `yaml:"-"`
	SecurityGroupName string `yaml:"-"`
}

func splitAtSlash(s string) string {
	parts := strings.Split(s, "/")
	return parts[0]
}

func filterRules(rules []Rule, filter func(*Rule) bool) []Rule {
	var filtered []Rule
	for i := range rules {
		if filter(&rules[i]) {
			filtered = append(filtered, rules[i])
		}
	}
	return filtered
}

func main() {
	var yamlFilePath string
	flag.StringVar(&yamlFilePath, "config", "network.yaml", "path to the YAML file")

	var firewallScript string
	flag.StringVar(&firewallScript, "script", "firewall.sh", "path to lxc container network config script")

	var terraformScript string
	flag.StringVar(&terraformScript, "terraform", "sg_rules.tf", "path output terraform script")

	var lxdConfig string
	flag.StringVar(&lxdConfig, "lxd", "lxd_config.yaml", "path output LXD config")

	var container string
	flag.StringVar(&container, "container", "csls", "name of the LXC container")

	var securityGroupName string
	flag.StringVar(&securityGroupName, "security-group-name", "northflier", "ID of the security group")

	flag.Parse()

	// Open the YAML file
	file, err := os.Open(yamlFilePath)
	if err != nil {
		fmt.Println("Failed to open YAML file:", err)
		return
	}
	defer file.Close()

	// Parse YAML data into the configuration struct
	config := Config{
		LXCName:           container,
		SecurityGroupName: securityGroupName,
	}
	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		fmt.Println("Failed to parse YAML:", err)
		return
	}

	// Filter the rules for LXC, LXD and Terraform
	terraformRules := filterRules(config.Rules, func(rule *Rule) bool {
		return true // All rules for Terraform
	})

	lxcRules := filterRules(config.Rules, func(rule *Rule) bool {
		// Adapt according to your needs
		if rule.Protocol == "icmp" || rule.Type == "egress" {
			return false
		}

		portRange := fmt.Sprintf(":%d-%d", rule.FromPort, rule.ToPort)
		if rule.FromPort == rule.ToPort {
			portRange = fmt.Sprintf(":%d", rule.FromPort)
		}
		rule.FormattedPortRange = portRange

		connectStr := fmt.Sprintf("%s:127.0.0.1:%d-%d", rule.Protocol, rule.FromPort, rule.ToPort)
		if rule.FromPort == rule.ToPort {
			connectStr = fmt.Sprintf("%s:127.0.0.1:%d", rule.Protocol, rule.FromPort)
		}

		if rule.LXCForward != 0 {
			connectStr = fmt.Sprintf("%s:127.0.0.1:%d", rule.Protocol, rule.LXCForward)
		}

		rule.LXCConnect = connectStr
		return true
	})

	config.Rules = terraformRules

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
		"join":         strings.Join,
		"splitAtSlash": splitAtSlash,
	}).Parse(lxcTemplate)
	if err != nil {
		fmt.Println("Failed to parse template:", err)
		return
	}

	lxdTmpl, err := template.New("lxd").Funcs(template.FuncMap{
		"splitAtSlash": splitAtSlash,
	}).Parse(lxdTemplate)
	if err != nil {
		fmt.Println("Failed to parse template:", err)
		return
	}

	// Create and open the output files
	outFile, err := os.Create(terraformScript)
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
	os.Chmod(firewallScript, 0o755)

	lxdOutFile, err := os.Create(lxdConfig)
	if err != nil {
		fmt.Println("Failed to create output file:", err)
		return
	}
	defer lxdOutFile.Close()

	// Execute templates
	err = tmpl.Execute(outFile, &config)
	if err != nil {
		fmt.Println("Failed to execute template:", err)
		return
	}

	config.Rules = lxcRules
	err = lxcTmpl.Execute(lxcOutFile, &config)
	if err != nil {
		fmt.Println("Failed to execute template:", err)
		return
	}

	err = lxdTmpl.Execute(lxdOutFile, &config)
	if err != nil {
		fmt.Println("Failed to execute template:", err)
		return
	}

	fmt.Println("Terraform, LXC and LXD configurations generated successfully")
}
