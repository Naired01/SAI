package inventory

import "errors"

// ErrInvalidSource se devuelve cuando el campo `source` no está entre los
// valores permitidos (agent | manual | seed).
var ErrInvalidSource = errors.New("inventory: invalid source")

// ErrEmptyCorrelationID se devuelve cuando un snapshot llega sin el campo
// `id` que debe ecoear la request original.
var ErrEmptyCorrelationID = errors.New("inventory: empty correlation id")

// ErrUnsupportedSchema rechaza snapshots cuyo schema_ver no coincide con
// la versión soportada actualmente. Esto evita que agentes muy viejos o muy
// nuevos envenenen la tabla con formatos incompatibles.
var ErrUnsupportedSchema = errors.New("inventory: unsupported schema version")

// ErrMissingHardware rechaza snapshots sin bloque hardware.
var ErrMissingHardware = errors.New("inventory: missing hardware block")
