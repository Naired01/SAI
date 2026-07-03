// Package groups administra grupos jerárquicos de agentes con soporte
// para subgrupos, validación anti-ciclos, miembros many-to-many y
// bulk-move atómico.
package groups

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Group representa un grupo (puede tener parent_id == nil → raíz).
type Group struct {
	ID          string   `json:"id"`
	ParentID    *string  `json:"parent_id,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Color       string   `json:"color,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	SortOrder   int      `json:"sort_order"`
	MemberCount int      `json:"member_count"`
	Children    []*Group `json:"children,omitempty"`
}

// Flat es la versión plana (sin anidar) usada en listados simples.
type Flat struct {
	ID          string  `json:"id"`
	ParentID    *string `json:"parent_id,omitempty"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Color       string  `json:"color,omitempty"`
	Icon        string  `json:"icon,omitempty"`
	SortOrder   int     `json:"sort_order"`
	MemberCount int     `json:"member_count"`
}

// CreateInput datos para crear un grupo.
type CreateInput struct {
	ParentID    *string `json:"parent_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Color       string  `json:"color"`
	Icon        string  `json:"icon"`
	SortOrder   int     `json:"sort_order"`
	CreatedBy   *string `json:"-"`
}

// Create crea un grupo nuevo. Valida que el parent exista y que el nombre
// no se duplique dentro del mismo padre.
func Create(ctx context.Context, pool *pgxpool.Pool, in CreateInput) (*Flat, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("name required")
	}
	if in.ParentID != nil && *in.ParentID != "" {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_groups WHERE id = $1)`, *in.ParentID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("parent group not found: %s", *in.ParentID)
		}
	}

	var g Flat
	row := pool.QueryRow(ctx, `
		INSERT INTO agent_groups (parent_id, name, description, color, icon, sort_order, created_by)
		VALUES (NULLIF($1,'')::uuid, $2, NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), $6, $7)
		RETURNING id, parent_id, name, COALESCE(description,''), COALESCE(color,''),
		          COALESCE(icon,''), sort_order
	`, derefStr(in.ParentID), name, in.Description, in.Color, in.Icon, in.SortOrder, in.CreatedBy)
	if err := row.Scan(&g.ID, &g.ParentID, &g.Name, &g.Description, &g.Color, &g.Icon, &g.SortOrder); err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("group name already exists in this parent")
		}
		return nil, err
	}
	return &g, nil
}

// Get devuelve un grupo por ID.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (*Flat, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, parent_id, name, COALESCE(description,''), COALESCE(color,''),
		       COALESCE(icon,''), sort_order,
		       (SELECT COUNT(*) FROM agent_group_members WHERE group_id = g.id)
		FROM agent_groups g WHERE id = $1
	`, id)
	g := &Flat{}
	if err := row.Scan(&g.ID, &g.ParentID, &g.Name, &g.Description, &g.Color, &g.Icon, &g.SortOrder, &g.MemberCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return g, nil
}

// UpdateInput cambios parciales.
type UpdateInput struct {
	ParentID    *string `json:"parent_id"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	Icon        *string `json:"icon"`
	SortOrder   *int    `json:"sort_order"`
}

// Update aplica cambios. Si se cambia parent_id valida anti-ciclo.
func Update(ctx context.Context, pool *pgxpool.Pool, id string, in UpdateInput) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if in.ParentID != nil {
		newParent := *in.ParentID
		if newParent == "" {
			// mover a raíz
			if _, err := tx.Exec(ctx, `UPDATE agent_groups SET parent_id = NULL, updated_at = now() WHERE id = $1`, id); err != nil {
				return err
			}
		} else {
			if newParent == id {
				return ErrCycle
			}
			// validar que newParent no sea descendiente de id
			if err := assertNotDescendant(ctx, tx, id, newParent); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE agent_groups SET parent_id = $1, updated_at = now() WHERE id = $2`, newParent, id); err != nil {
				return err
			}
		}
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return errors.New("name required")
		}
		if _, err := tx.Exec(ctx, `UPDATE agent_groups SET name = $1, updated_at = now() WHERE id = $2`, name, id); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("group name already exists in this parent")
			}
			return err
		}
	}
	if in.Description != nil {
		if _, err := tx.Exec(ctx, `UPDATE agent_groups SET description = NULLIF($1,''), updated_at = now() WHERE id = $2`, *in.Description, id); err != nil {
			return err
		}
	}
	if in.Color != nil {
		if _, err := tx.Exec(ctx, `UPDATE agent_groups SET color = NULLIF($1,''), updated_at = now() WHERE id = $2`, *in.Color, id); err != nil {
			return err
		}
	}
	if in.Icon != nil {
		if _, err := tx.Exec(ctx, `UPDATE agent_groups SET icon = NULLIF($1,''), updated_at = now() WHERE id = $2`, *in.Icon, id); err != nil {
			return err
		}
	}
	if in.SortOrder != nil {
		if _, err := tx.Exec(ctx, `UPDATE agent_groups SET sort_order = $1, updated_at = now() WHERE id = $2`, *in.SortOrder, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Delete elimina un grupo (cascade borra subgrupos y miembros).
func Delete(ctx context.Context, pool *pgxpool.Pool, id string) error {
	tag, err := pool.Exec(ctx, `DELETE FROM agent_groups WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Tree devuelve el árbol completo de grupos con member_count.
func Tree(ctx context.Context, pool *pgxpool.Pool) ([]*Group, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, parent_id, name, COALESCE(description,''), COALESCE(color,''),
		       COALESCE(icon,''), sort_order,
		       (SELECT COUNT(*) FROM agent_group_members WHERE group_id = g.id)
		FROM agent_groups g ORDER BY sort_order, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	flat := []*Flat{}
	for rows.Next() {
		g := &Flat{}
		if err := rows.Scan(&g.ID, &g.ParentID, &g.Name, &g.Description, &g.Color, &g.Icon, &g.SortOrder, &g.MemberCount); err != nil {
			return nil, err
		}
		flat = append(flat, g)
	}
	return buildTree(flat), nil
}

// AddMembers agrega uno o varios agentes al grupo.
func AddMembers(ctx context.Context, pool *pgxpool.Pool, groupID string, agentIDs []string, actorID *string) (added int, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, aid := range agentIDs {
		ct, err := tx.Exec(ctx, `
			INSERT INTO agent_group_members (agent_id, group_id, added_by)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, aid, groupID, actorID)
		if err != nil {
			return 0, err
		}
		added += int(ct.RowsAffected())
	}
	return added, tx.Commit(ctx)
}

// RemoveMember quita un agente del grupo.
func RemoveMember(ctx context.Context, pool *pgxpool.Pool, groupID, agentID string) error {
	_, err := pool.Exec(ctx, `DELETE FROM agent_group_members WHERE group_id = $1 AND agent_id = $2`, groupID, agentID)
	return err
}

// BulkMoveInput mueve N agentes a un grupo destino.
type BulkMoveInput struct {
	AgentIDs []string `json:"agent_ids"`
	GroupID  *string  `json:"group_id"` // nil → quitar de todos los grupos
	ActorID  *string  `json:"-"`
}

// BulkMove mueve agentes a un grupo destino (atómico). Si GroupID es nil,
// los agentes se quitan de todos los grupos.
func BulkMove(ctx context.Context, pool *pgxpool.Pool, in BulkMoveInput) error {
	if len(in.AgentIDs) == 0 {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if in.GroupID == nil || *in.GroupID == "" {
		// quitar de todos
		_, err := tx.Exec(ctx, `DELETE FROM agent_group_members WHERE agent_id = ANY($1)`, in.AgentIDs)
		if err != nil {
			return err
		}
	} else {
		// quitar de los demás y agregar al destino (idempotente)
		if _, err := tx.Exec(ctx, `DELETE FROM agent_group_members WHERE agent_id = ANY($1) AND group_id <> $2`,
			in.AgentIDs, *in.GroupID); err != nil {
			return err
		}
		for _, aid := range in.AgentIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO agent_group_members (agent_id, group_id, added_by)
				VALUES ($1, $2, $3)
				ON CONFLICT DO NOTHING
			`, aid, *in.GroupID, in.ActorID); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// assertNotDescendant devuelve ErrCycle si `descendant` es descendiente
// (en cualquier nivel) de `ancestor`.
func assertNotDescendant(ctx context.Context, tx pgx.Tx, ancestor, descendant string) error {
	current := descendant
	for i := 0; i < 1000; i++ { // tope defensivo
		var parent *string
		err := tx.QueryRow(ctx, `SELECT parent_id FROM agent_groups WHERE id = $1`, current).Scan(&parent)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if parent == nil {
			return nil
		}
		if *parent == ancestor {
			return ErrCycle
		}
		current = *parent
	}
	return errors.New("tree too deep")
}

func buildTree(flat []*Flat) []*Group {
	byID := map[string]*Group{}
	for _, f := range flat {
		g := &Group{
			ID:          f.ID,
			ParentID:    f.ParentID,
			Name:        f.Name,
			Description: f.Description,
			Color:       f.Color,
			Icon:        f.Icon,
			SortOrder:   f.SortOrder,
			MemberCount: f.MemberCount,
		}
		byID[g.ID] = g
	}
	var roots []*Group
	for _, f := range flat {
		g := byID[f.ID]
		if f.ParentID == nil || *f.ParentID == "" {
			roots = append(roots, g)
			continue
		}
		parent, ok := byID[*f.ParentID]
		if !ok {
			// huérfano: trátalo como raíz
			roots = append(roots, g)
			continue
		}
		parent.Children = append(parent.Children, g)
	}
	return roots
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// isUniqueViolation chequea el SQLSTATE 23505 de Postgres.
func isUniqueViolation(err error) bool {
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
	ErrNotFound = errors.New("group not found")
	ErrCycle    = errors.New("cycle detected in group hierarchy")
)