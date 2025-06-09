package utils

// CheckForError takes any input and returns the error if it is one, otherwise nil.
func CheckForError(input interface{}) error {
	err, ok := input.(error)
	if ok {
		return err
	}
	return nil
}
