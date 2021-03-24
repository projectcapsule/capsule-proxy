package request

type Request interface {
	GetUserAndGroups() (string, []string, error)
}
