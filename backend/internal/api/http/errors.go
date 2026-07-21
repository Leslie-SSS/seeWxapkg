package httpapi

type badRequestError struct {
	message string
}

func (e badRequestError) Error() string {
	return e.message
}

func httpError(message string) error {
	return badRequestError{message: message}
}
