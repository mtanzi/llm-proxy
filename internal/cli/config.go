package cli

import (
	"context"

	"github.com/mtanzi/spyglass/internal/toon"
)

func (a *app) config(ctx context.Context, args []string) (toon.Object, error) {
	const cmd = "config"
	fs := newFlagSet(cmd)
	help := func() string {
		return helpText(fs, "spyglass config",
			"Providers (name, kind, auth_mode, base_url), pricing per model and start day, and virtual key names with the disabled flag. It holds no credential and no key hash.",
			"spyglass config")
	}
	pos, err := parseFlags(fs, cmd, args, help)
	if err != nil {
		return nil, err
	}
	if err := noArgs(cmd, pos); err != nil {
		return nil, err
	}
	var cfg configView
	if err := a.get(ctx, "/admin/config", nil, &cfg); err != nil {
		return nil, err
	}
	return toon.Object{
		{Key: "providers", Value: tableOf(providerCols.cols, cfg.Providers)},
		{Key: "pricing", Value: tableOf(pricingCols.cols, cfg.Pricing)},
		{Key: "keys", Value: tableOf(keyCols.cols, cfg.Keys)},
		helpPair([]string{"spyglass describe", "spyglass calls list --since 24h"}),
	}, nil
}
