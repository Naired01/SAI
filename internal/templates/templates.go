// Package templates administra plantillas de comando reutilizables,
// organizadas por categoría. Incluye un seed inicial con plantillas
// builtin (no editables) útiles para diagnóstico y mantenimiento.
package templates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Template representa una plantilla de comando.
type Template struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Description       string   `json:"description,omitempty"`
	Category          string   `json:"category"`
	Command           string   `json:"command"`
	Args              []string `json:"args"`
	WorkingDir        string   `json:"working_dir,omitempty"`
	TimeoutSeconds    int      `json:"timeout_seconds"`
	RequiresElevation bool     `json:"requires_elevation"`
	RequiresConfirm   bool     `json:"requires_confirm"`
	IsBuiltin         bool     `json:"is_builtin"`
	ShowInDashboard   bool     `json:"show_in_dashboard"`
	Icon              string   `json:"icon,omitempty"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

// CreateInput body para POST /templates.
type CreateInput struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Category          string   `json:"category"`
	Command           string   `json:"command"`
	Args              []string `json:"args"`
	WorkingDir        string   `json:"working_dir"`
	TimeoutSeconds    int      `json:"timeout_seconds"`
	RequiresElevation bool     `json:"requires_elevation"`
	RequiresConfirm   bool     `json:"requires_confirm"`
	ShowInDashboard   bool     `json:"show_in_dashboard"`
	Icon              string   `json:"icon"`
	CreatedBy         *string  `json:"-"`
}

// UpdateInput body para PATCH /templates/{id}.
type UpdateInput struct {
	Name              *string  `json:"name"`
	Description       *string  `json:"description"`
	Category          *string  `json:"category"`
	Command           *string  `json:"command"`
	Args              []string `json:"args"`
	WorkingDir        *string  `json:"working_dir"`
	TimeoutSeconds    *int     `json:"timeout_seconds"`
	RequiresElevation *bool    `json:"requires_elevation"`
	RequiresConfirm   *bool    `json:"requires_confirm"`
	ShowInDashboard   *bool    `json:"show_in_dashboard"`
	Icon              *string  `json:"icon"`
}

// Create crea una plantilla nueva.
func Create(ctx context.Context, pool *pgxpool.Pool, in CreateInput) (*Template, error) {
	if err := validateCreate(in); err != nil {
		return nil, err
	}
	argsJSON, _ := json.Marshal(orEmpty(in.Args))
	var t Template
	row := pool.QueryRow(ctx, `
		INSERT INTO command_templates (name, description, category, command, args,
			working_dir, timeout_seconds, requires_elevation, requires_confirm,
			is_builtin, show_in_dashboard, icon, created_by)
		VALUES ($1, NULLIF($2,''), $3, $4, $5::jsonb, NULLIF($6,''), $7, $8, $9, FALSE, $10, NULLIF($11,''), $12)
		RETURNING id, name, COALESCE(description,''), category, command, args,
		          COALESCE(working_dir,''), timeout_seconds, requires_elevation,
		          requires_confirm, is_builtin, show_in_dashboard, COALESCE(icon,''),
		          created_at, updated_at
	`, in.Name, in.Description, orDefault(in.Category, "general"), in.Command, string(argsJSON),
		in.WorkingDir, orDefault(in.TimeoutSeconds, 60), in.RequiresElevation, in.RequiresConfirm,
		in.ShowInDashboard, in.Icon, in.CreatedBy)
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Category, &t.Command, &argsJSON,
		&t.WorkingDir, &t.TimeoutSeconds, &t.RequiresElevation, &t.RequiresConfirm,
		&t.IsBuiltin, &t.ShowInDashboard, &t.Icon, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if isUnique(err) {
			return nil, fmt.Errorf("template name already exists")
		}
		return nil, err
	}
	t.Args = unmarshalArgs(argsJSON)
	return &t, nil
}

// ListOptions filtros.
type ListOptions struct {
	Category string
	Search   string
}

// List devuelve todas las plantillas (orden por categoría, nombre).
func List(ctx context.Context, pool *pgxpool.Pool, opts ListOptions) ([]*Template, error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if opts.Category != "" {
		where = append(where, fmt.Sprintf("category = $%d", idx))
		args = append(args, opts.Category)
		idx++
	}
	if s := strings.TrimSpace(opts.Search); s != "" {
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR description ILIKE $%d)", idx, idx))
		args = append(args, "%"+s+"%")
		idx++
	}
	q := `SELECT id, name, COALESCE(description,''), category, command, args,
	             COALESCE(working_dir,''), timeout_seconds, requires_elevation,
	             requires_confirm, is_builtin, show_in_dashboard, COALESCE(icon,''),
	             created_at, updated_at
	      FROM command_templates WHERE ` + strings.Join(where, " AND ") + `
	      ORDER BY category, name`
	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Template
	for rows.Next() {
		var (
			t  Template
			ab []byte
		)
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.Category, &t.Command, &ab,
			&t.WorkingDir, &t.TimeoutSeconds, &t.RequiresElevation, &t.RequiresConfirm,
			&t.IsBuiltin, &t.ShowInDashboard, &t.Icon, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Args = unmarshalArgs(ab)
		out = append(out, &t)
	}
	return out, rows.Err()
}

// Get devuelve una plantilla por ID.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (*Template, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, name, COALESCE(description,''), category, command, args,
		       COALESCE(working_dir,''), timeout_seconds, requires_elevation,
		       requires_confirm, is_builtin, show_in_dashboard, COALESCE(icon,''),
		       created_at, updated_at
		FROM command_templates WHERE id = $1
	`, id)
	var (
		t  Template
		ab []byte
	)
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Category, &t.Command, &ab,
		&t.WorkingDir, &t.TimeoutSeconds, &t.RequiresElevation, &t.RequiresConfirm,
		&t.IsBuiltin, &t.ShowInDashboard, &t.Icon, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.Args = unmarshalArgs(ab)
	return &t, nil
}

// Update aplica cambios parciales; rechaza is_builtin=true.
func Update(ctx context.Context, pool *pgxpool.Pool, id string, in UpdateInput) error {
	t, err := Get(ctx, pool, id)
	if err != nil {
		return err
	}
	if t.IsBuiltin {
		return ErrBuiltin
	}
	if in.TimeoutSeconds != nil && (*in.TimeoutSeconds < 1 || *in.TimeoutSeconds > 86400) {
		return fmt.Errorf("timeout_seconds must be 1..86400")
	}
	argsJSON, _ := json.Marshal(orEmpty(in.Args))
	_, err = pool.Exec(ctx, `
		UPDATE command_templates SET
			name              = COALESCE($1, name),
			description       = COALESCE($2, description),
			category          = COALESCE($3, category),
			command           = COALESCE($4, command),
			args              = COALESCE($5::jsonb, args),
			working_dir       = COALESCE($6, working_dir),
			timeout_seconds   = COALESCE($7, timeout_seconds),
			requires_elevation= COALESCE($8, requires_elevation),
			requires_confirm  = COALESCE($9, requires_confirm),
			show_in_dashboard = COALESCE($10, show_in_dashboard),
			icon              = COALESCE($11, icon),
			updated_at        = now()
		WHERE id = $12 AND is_builtin = FALSE
	`, in.Name, in.Description, in.Category, in.Command,
		func() any {
			if in.Args == nil {
				return nil
			}
			return string(argsJSON)
		}(),
		in.WorkingDir, in.TimeoutSeconds, in.RequiresElevation, in.RequiresConfirm,
		in.ShowInDashboard, in.Icon, id)
	if err != nil {
		if isUnique(err) {
			return fmt.Errorf("template name already exists")
		}
	}
	return err
}

// Delete elimina; rechaza is_builtin=true.
func Delete(ctx context.Context, pool *pgxpool.Pool, id string) error {
	tag, err := pool.Exec(ctx, `DELETE FROM command_templates WHERE id = $1 AND is_builtin = FALSE`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// distinguir 404 de builtin
		t, gErr := Get(ctx, pool, id)
		if gErr != nil {
			return gErr
		}
		if t.IsBuiltin {
			return ErrBuiltin
		}
		return ErrNotFound
	}
	return nil
}

// -----------------------------------------------------------------------------
// Seed de plantillas builtin
// -----------------------------------------------------------------------------

// SeedBuiltins inserta las plantillas builtin si no existen (idempotente).
// Llamar al arrancar el server.
func SeedBuiltins(ctx context.Context, pool *pgxpool.Pool) error {
	builtins := []*CreateInput{
		{
			Name:            "ipconfig /all",
			Description:     "Muestra la configuración de red del equipo.",
			Category:        "diagnostics",
			Command:         "ipconfig",
			Args:            []string{"/all"},
			TimeoutSeconds:  30,
			ShowInDashboard: true,
			Icon:            "network",
		},
		{
			Name:            "systeminfo",
			Description:     "Resumen del sistema operativo y hardware.",
			Category:        "diagnostics",
			Command:         "systeminfo",
			TimeoutSeconds:  60,
			ShowInDashboard: true,
			Icon:            "cpu",
		},
		{
			Name:            "df -h (Linux/macOS)",
			Description:     "Uso de discos.",
			Category:        "diagnostics",
			Command:         "df",
			Args:            []string{"-h"},
			TimeoutSeconds:  30,
			Icon:            "hard-drive",
		},
		{
			Name:              "netstat -an",
			Description:       "Conexiones y puertos abiertos.",
			Category:          "diagnostics",
			Command:           "netstat",
			Args:              []string{"-an"},
			TimeoutSeconds:    30,
			RequiresElevation: false,
			Icon:              "activity",
		},
		{
			Name:            "Get-HotFix (parches)",
			Description:     "Lista los parches instalados en Windows.",
			Category:        "security",
			Command:         "Get-HotFix",
			TimeoutSeconds:  60,
			Icon:            "shield",
		},
		{
			Name:            "Listar servicios",
			Description:     "Lista los servicios del sistema (Windows).",
			Category:        "security",
			Command:         "Get-Service",
			TimeoutSeconds:  60,
			ShowInDashboard: true,
			Icon:            "list",
		},
	}
	for _, b := range builtins {
		argsJSON, _ := json.Marshal(orEmpty(b.Args))
		_, err := pool.Exec(ctx, `
			INSERT INTO command_templates
			    (name, description, category, command, args, working_dir, timeout_seconds,
			     requires_elevation, requires_confirm, is_builtin, show_in_dashboard, icon)
			VALUES ($1, $2, $3, $4, $5::jsonb, NULLIF($6,''), $7, $8, $9, TRUE, $10, NULLIF($11,''))
			ON CONFLICT (name) DO NOTHING
		`, b.Name, b.Description, b.Category, b.Command, string(argsJSON), b.WorkingDir,
			b.TimeoutSeconds, b.RequiresElevation, b.RequiresConfirm, b.ShowInDashboard, b.Icon)
		if err != nil {
			return fmt.Errorf("seed %s: %w", b.Name, err)
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func validateCreate(in CreateInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("name required")
	}
	if strings.TrimSpace(in.Command) == "" {
		return errors.New("command required")
	}
	if in.TimeoutSeconds < 0 || in.TimeoutSeconds > 86400 {
		return fmt.Errorf("timeout_seconds must be 0..86400 (got %d)", in.TimeoutSeconds)
	}
	return nil
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orDefault[T int | string](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

func unmarshalArgs(b []byte) []string {
	if len(b) == 0 {
		return []string{}
	}
	var out []string
	_ = json.Unmarshal(b, &out)
	if out == nil {
		return []string{}
	}
	return out
}

func isUnique(err error) bool {
	if err == nil {
		return false
	}
	type sqlStater interface{ SQLState() string }
	var s sqlStater
	if errors.As(err, &s) {
		return s.SQLState() == "23505"
	}
	return false
}

// Errores públicos.
var (
	ErrNotFound = errors.New("template not found")
	ErrBuiltin  = errors.New("builtin templates are read-only")
)