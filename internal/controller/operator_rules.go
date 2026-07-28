package controller

// This file defines the extension point for fortiConfig.Spec.OperatorRule.
//
// TO ADD SUPPORT FOR A NEW OPERATOR RULE:
//  1. Add your Terraform template file (with its <FGT_XXX> placeholders and
//     any <FGT_FOR_STR> loops) wherever the other templates live.
//  2. Create a new rule_<name>.go file implementing OperatorRuleHandler
//     (or just a plain function via OperatorRuleFunc - see rule_vip.go and
//     rule_interface.go for the two styles).
//  3. Register it from an init() in that same file:
//
//       func init() { RegisterOperatorRule("my_new_rule", OperatorRuleFunc(applyMyNewRule)) }
//
// That's it - selectOperatorRule (and every other existing file) never
// needs to change. Go's init() ordering guarantees every rule_*.go file's
// init() runs before main()/the controller starts handling requests, as
// long as the file lives in this package and the package is imported.
import (
	"fmt"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
)

// OperatorRuleHandler applies one fortiConfig.Spec.OperatorRule value's
// worth of template substitutions.
//
// baseFile is the already-copied-and-FOR-loop-expanded (smartNestedProcess)
// template file for this CR, i.e. "<workingDir>/<cfg.Name>_<template>".
// Implementations whose CR describes a single resource instance modify
// baseFile directly; implementations whose CR contains a list (one CR ->
// many Terraform resources) derive one file path per element - see
// IndexedResourceFile in rule_helpers.go - and modify each in turn.
type OperatorRuleHandler interface {
	Apply(cfg k8sdinovaonev1.FortigateConfig, baseFile string) error
}

// OperatorRuleFunc lets a plain function satisfy OperatorRuleHandler
// without declaring a named type for it - the common case for a simple,
// single-purpose rule (see rule_vip.go / rule_interface.go / rule_fwrules.go).
type OperatorRuleFunc func(cfg k8sdinovaonev1.FortigateConfig, baseFile string) error

func (f OperatorRuleFunc) Apply(cfg k8sdinovaonev1.FortigateConfig, baseFile string) error {
	return f(cfg, baseFile)
}

var operatorRuleRegistry = map[string]OperatorRuleHandler{}

// RegisterOperatorRule makes a new fortiConfig.Spec.OperatorRule value
// available to selectOperatorRule. Call it from an init() next to your
// handler's definition. Panics on duplicate registration - two rule files
// claiming the same name is always a build-time mistake, not something
// worth surviving at runtime (fail fast, at startup, rather than silently
// having the second registration lost).
func RegisterOperatorRule(name string, handler OperatorRuleHandler) {
	if name == "" {
		panic("controller: RegisterOperatorRule called with an empty name")
	}
	if _, exists := operatorRuleRegistry[name]; exists {
		panic(fmt.Sprintf("controller: operator rule %q registered more than once", name))
	}
	operatorRuleRegistry[name] = handler
}

func lookupOperatorRule(name string) (OperatorRuleHandler, bool) {
	h, ok := operatorRuleRegistry[name]
	return h, ok
}

// KnownOperatorRules returns the currently-registered rule names, mainly
// useful for a clearer error message and for a status/debug endpoint.
func KnownOperatorRules() []string {
	names := make([]string, 0, len(operatorRuleRegistry))
	for name := range operatorRuleRegistry {
		names = append(names, name)
	}
	return names
}