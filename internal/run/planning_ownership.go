package run

func maxTicketsForComplexity(complexity string) int {
	switch complexity {
	case "xs", "s":
		return 1
	case "m":
		return 3
	case "l":
		return 6
	case "xl":
		return 12
	default:
		return 1
	}
}

func ticketsAreOrdered(a, b plannedTicket) bool {
	for _, dependency := range a.DependsOnTitles {
		if dependency == b.Title {
			return true
		}
	}
	for _, dependency := range b.DependsOnTitles {
		if dependency == a.Title {
			return true
		}
	}
	return false
}

func overlappingOwnedPath(a, b []string) string {
	for _, left := range a {
		for _, right := range b {
			if left != "" && left == right {
				return left
			}
		}
	}
	return ""
}
