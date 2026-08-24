package mermaid

type direction genericCoord

var (
	Up         = direction{1, 0}
	Down       = direction{1, 2}
	Left       = direction{0, 1}
	Right      = direction{2, 1}
	UpperRight = direction{2, 0}
	UpperLeft  = direction{0, 0}
	LowerRight = direction{2, 2}
	LowerLeft  = direction{0, 2}
	Middle     = direction{1, 1}
)

func (d direction) getOpposite() direction {
	switch d {
	case Up:
		return Down
	case Down:
		return Up
	case Left:
		return Right
	case Right:
		return Left
	case UpperRight:
		return LowerLeft
	case UpperLeft:
		return LowerRight
	case LowerRight:
		return UpperLeft
	case LowerLeft:
		return UpperRight
	case Middle:
		return Middle
	}
	panic("Unknown direction")
}

func (c gridCoord) Direction(dir direction) gridCoord {
	return gridCoord{x: c.x + dir.x, y: c.y + dir.y}
}

func (g graph) selfReferenceDirection() (direction, direction, direction, direction) {
	if g.graphDirection == "LR" {
		return Right, Down, Down, Right
	}
	return Down, Right, Right, Down
}

func (g graph) determineStartAndEndDir(e *edge) (direction, direction, direction, direction) {
	if e.from == e.to {
		return g.selfReferenceDirection()
	}
	d := determineDirection(genericCoord(*e.from.gridCoord), genericCoord(*e.to.gridCoord))
	var preferredDir, preferredOppositeDir, alternativeDir, alternativeOppositeDir direction

	isBackwards := (g.graphDirection == "LR" && (d == Left || d == UpperLeft || d == LowerLeft)) ||
		(g.graphDirection != "LR" && (d == Up || d == UpperLeft || d == UpperRight))

	// TODO: This causes some squirmy lines if the corner spot is already occupied.
	// For backwards edges, use special start positions: Down in LR mode, Right in TD mode
	switch d {
	case LowerRight:
		if g.graphDirection == "LR" {
			preferredDir = Down
			preferredOppositeDir = Left
			alternativeDir = Right
			alternativeOppositeDir = Up
		} else {
			preferredDir = Right
			preferredOppositeDir = Up
			alternativeDir = Down
			alternativeOppositeDir = Left
		}
	case UpperRight:
		if g.graphDirection == "LR" {
			preferredDir = Up
			preferredOppositeDir = Left
			alternativeDir = Right
			alternativeOppositeDir = Down
		} else {
			preferredDir = Right
			preferredOppositeDir = Down
			alternativeDir = Up
			alternativeOppositeDir = Left
		}
	case LowerLeft:
		if g.graphDirection == "LR" {
			preferredDir = Down
			preferredOppositeDir = Down
			alternativeDir = Left
			alternativeOppositeDir = Up
		} else {
			preferredDir = Left
			preferredOppositeDir = Up
			alternativeDir = Down
			alternativeOppositeDir = Right
		}
	case UpperLeft:
		if g.graphDirection == "LR" {
			preferredDir = Down
			preferredOppositeDir = Down
			alternativeDir = Left
			alternativeOppositeDir = Down
		} else {
			preferredDir = Right
			preferredOppositeDir = Right
			alternativeDir = Up
			alternativeOppositeDir = Right
		}
	default:
		if isBackwards {
			if g.graphDirection == "LR" && d == Left {
				preferredDir = Down
				preferredOppositeDir = Down
				alternativeDir = Left
				alternativeOppositeDir = Right
			} else if g.graphDirection == "TD" && d == Up {
				preferredDir = Right
				preferredOppositeDir = Right
				alternativeDir = Up
				alternativeOppositeDir = Down
			} else {
				preferredDir = d
				preferredOppositeDir = preferredDir.getOpposite()
				alternativeDir = d
				alternativeOppositeDir = preferredOppositeDir
			}
		} else {
			preferredDir = d
			preferredOppositeDir = preferredDir.getOpposite()
			// TODO: just return null and don't calculate alternative path
			alternativeDir = d
			alternativeOppositeDir = preferredOppositeDir
		}
	}
	return preferredDir, preferredOppositeDir, alternativeDir, alternativeOppositeDir
}
