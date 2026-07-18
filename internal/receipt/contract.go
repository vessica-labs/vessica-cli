package receipt

const (
	FieldElapsed        = "elapsed"
	FieldWallElapsed    = "wall_elapsed"
	FieldInfrastructure = "infrastructure"
)

// ContextContract is the compact, version-matched receipt contract supplied to
// coding agents when a ticket mentions run receipts. Keeping the field names
// beside the receipt implementation prevents agents from rediscovering them
// through web searches or a second repository clone.
func ContextContract() string {
	return "ves run receipt <run_id> --json returns the completed-run receipt under data.body. " +
		"Execution elapsed is data.body." + FieldElapsed + "; request-to-finish elapsed is data.body." + FieldWallElapsed +
		"; infrastructure stage spans are data.body." + FieldInfrastructure + "."
}
