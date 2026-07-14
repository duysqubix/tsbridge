package app

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/jtdowney/tsbridge/internal/config"
)

// serviceRegistryOps is the minimal interface for dynamic config reloads.
// It supports adding, removing, and updating services at runtime.
type serviceRegistryOps interface {
	AddService(svcCfg config.Service) error
	RemoveService(name string) error
	UpdateService(name string, newCfg config.Service) error
}

// reloadConfigWithRegistry reloads services in the registry to match newCfg.
// Removes, adds, and updates services as needed, in that order.
// Continues on errors and joins every failure in the returned error.
func reloadConfigWithRegistry(oldCfg, newCfg *config.Config, registry serviceRegistryOps) error {
	toRemove := findServicesToRemove(oldCfg, newCfg)
	toAdd := findServicesToAdd(oldCfg, newCfg)
	toUpdate := findServicesToUpdate(oldCfg, newCfg)

	if len(toRemove) > 0 || len(toAdd) > 0 || len(toUpdate) > 0 {
		slog.Info("configuration changes detected",
			"services_to_remove", len(toRemove),
			"services_to_add", len(toAdd),
			"services_to_update", len(toUpdate))
	} else {
		slog.Info("no service configuration changes detected")
		return nil
	}

	var failures []error
	successful := 0
	removeFailures := 0
	addFailures := 0
	updateFailures := 0

	for _, name := range toRemove {
		if err := registry.RemoveService(name); err != nil {
			slog.Error("failed to remove service",
				"service", name,
				"error", err,
				"operation", "reload_remove")
			failures = append(failures, fmt.Errorf("remove service %q: %w", name, err))
			removeFailures++
		} else {
			slog.Info("removed service during reload",
				"service", name,
				"operation", "reload_remove")
			successful++
		}
	}

	for _, svc := range toAdd {
		if err := registry.AddService(svc); err != nil {
			slog.Error("failed to add service",
				"service", svc.Name,
				"error", err,
				"operation", "reload_add",
				"backend", svc.BackendAddr)
			failures = append(failures, fmt.Errorf("add service %q: %w", svc.Name, err))
			addFailures++
		} else {
			slog.Info("added service during reload",
				"service", svc.Name,
				"operation", "reload_add",
				"backend", svc.BackendAddr)
			successful++
		}
	}

	for _, svc := range toUpdate {
		if err := registry.UpdateService(svc.Name, svc); err != nil {
			slog.Error("failed to update service",
				"service", svc.Name,
				"error", err,
				"operation", "reload_update",
				"backend", svc.BackendAddr)
			failures = append(failures, fmt.Errorf("update service %q: %w", svc.Name, err))
			updateFailures++
		} else {
			slog.Info("updated service during reload",
				"service", svc.Name,
				"operation", "reload_update",
				"backend", svc.BackendAddr)
			successful++
		}
	}

	if len(failures) > 0 {
		slog.Warn("configuration reload completed with errors",
			"successful_operations", successful,
			"failed_operations", len(failures),
			"add_errors", addFailures,
			"remove_errors", removeFailures,
			"update_errors", updateFailures)
	} else {
		slog.Info("configuration reload completed successfully",
			"operations", successful)
	}

	return errors.Join(failures...)
}
