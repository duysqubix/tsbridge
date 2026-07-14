package app

import (
	"log/slog"

	"github.com/jtdowney/tsbridge/internal/config"
	"github.com/jtdowney/tsbridge/internal/errors"
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
// Continues on errors and returns a ReloadError if any occur.
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

	reloadErr := errors.NewReloadError()

	for _, name := range toRemove {
		if err := registry.RemoveService(name); err != nil {
			slog.Error("failed to remove service",
				"service", name,
				"error", err,
				"operation", "reload_remove")
			reloadErr.RecordRemoveError(name, err)
		} else {
			slog.Info("removed service during reload",
				"service", name,
				"operation", "reload_remove")
			reloadErr.RecordSuccess()
		}
	}

	for _, svc := range toAdd {
		if err := registry.AddService(svc); err != nil {
			slog.Error("failed to add service",
				"service", svc.Name,
				"error", err,
				"operation", "reload_add",
				"backend", svc.BackendAddr)
			reloadErr.RecordAddError(svc.Name, err)
		} else {
			slog.Info("added service during reload",
				"service", svc.Name,
				"operation", "reload_add",
				"backend", svc.BackendAddr)
			reloadErr.RecordSuccess()
		}
	}

	for _, svc := range toUpdate {
		if err := registry.UpdateService(svc.Name, svc); err != nil {
			slog.Error("failed to update service",
				"service", svc.Name,
				"error", err,
				"operation", "reload_update",
				"backend", svc.BackendAddr)
			reloadErr.RecordUpdateError(svc.Name, err)
		} else {
			slog.Info("updated service during reload",
				"service", svc.Name,
				"operation", "reload_update",
				"backend", svc.BackendAddr)
			reloadErr.RecordSuccess()
		}
	}

	if reloadErr.HasErrors() {
		slog.Warn("configuration reload completed with errors",
			"successful_operations", reloadErr.Successful,
			"failed_operations", reloadErr.Failed,
			"add_errors", len(reloadErr.AddErrors),
			"remove_errors", len(reloadErr.RemoveErrors),
			"update_errors", len(reloadErr.UpdateErrors))
	} else {
		slog.Info("configuration reload completed successfully",
			"operations", reloadErr.Successful)
	}

	return reloadErr.ToError()
}
