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

	// Filter the rules for LXC and Terraform
	terraformRules := filterRules(config.Rules, func(rule *Rule) bool {
		return true // All rules for Terraform
	})

	lxcRules := filterRules(config.Rules, func(rule *Rule) bool {
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
	err = os.Chmod(firewallScript, 0o755)
	if err != nil {
		fmt.Printf("Failed to set execute bit on %s: %s\n", firewallScript, err)
	}

	// Execute the templates and write the results to respective files
	err = tmpl.Execute(outFile, &Config{Rules: terraformRules, LXCName: config.LXCName, SecurityGroupName: config.SecurityGroupName})
	if err != nil {
		fmt.Println("Failed to execute template:", err)
		return
	}

	err = lxcTmpl.Execute(lxcOutFile, &Config{Rules: lxcRules, LXCName: config.LXCName})
	if err != nil {
		fmt.Println("Failed to execute LXC template:", err)
		return
	}
}
