package common

type TagList []string

func (lst TagList) Matches(other TagList) bool {
	otherMap := make(map[string]bool)
	for _, k := range other {
		otherMap[k] = true
	}

	for _, k := range lst {
		if _, ok := otherMap[k]; !ok {
			return false
		}
	}

	return true
}
