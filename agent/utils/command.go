package utils

const GryphCommandPlaceholder = "__GRYPH_COMMAND__"

// GryphCommand returns the gryph executable command.
// Currently returns "gryph", assuming it's in PATH.
func GryphCommand() string {
	return "gryph"
}
