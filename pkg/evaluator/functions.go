package evaluator

import (
	"fmt"
	"github.com/spyzhov/ajson"
	"time"
)

func _strings(left, right *ajson.Node) (lnum, rnum string, err error) {
	lnum, err = left.GetString()
	if err != nil {
		return
	}
	rnum, err = right.GetString()
	return
}

func _floats(left, right *ajson.Node) (lnum, rnum float64, err error) {
	lnum, err = left.GetNumeric()
	if err != nil {
		return
	}
	rnum, err = right.GetNumeric()
	return
}

func init() {
	ajson.AddOperation("-", 3, false, func(left *ajson.Node, right *ajson.Node) (result *ajson.Node, err error) {
		if left.IsString() {
			lNum, rNum, err := _strings(left, right)
			if err != nil {
				return nil, err
			}

			lDur, lDurErr := time.ParseDuration(lNum)
			rDur, rDurErr := time.ParseDuration(rNum)

			if timeL, err := time.Parse(time.RFC3339, lNum); err == nil && rDurErr == nil {
				// time is left, duration is right
				return ajson.StringNode("sub", timeL.Add(-rDur).Format(time.RFC3339)), nil
			} else if timeR, err := time.Parse(time.RFC3339, rNum); err == nil && lDurErr == nil {
				// time is right, duration is left
				return ajson.StringNode("sub", timeR.Add(-lDur).Format(time.RFC3339)), nil
			} else if lDurErr == nil && rDurErr == nil {
				// both sides durations
				return ajson.StringNode("sub", fmt.Sprintf("%vs", lDur.Seconds()-rDur.Seconds())), nil
			}
		}

		lNum, rNum, err := _floats(left, right)
		if err != nil {
			return
		}
		return ajson.NumericNode("sub", lNum-rNum), nil
	})
	ajson.AddOperation("+", 3, false, func(left *ajson.Node, right *ajson.Node) (result *ajson.Node, err error) {
		if left.IsString() {
			lNum, rNum, err := _strings(left, right)
			if err != nil {
				return nil, err
			}

			lDur, lDurErr := time.ParseDuration(lNum)
			rDur, rDurErr := time.ParseDuration(rNum)

			if timeL, err := time.Parse(time.RFC3339, lNum); err == nil && rDurErr == nil {

				// time is left, duration is right
				return ajson.StringNode("sum", timeL.Add(rDur).Format(time.RFC3339)), nil
			} else if timeR, err := time.Parse(time.RFC3339, rNum); err == nil && lDurErr == nil {

				// time is right, duration is left
				return ajson.StringNode("sum", timeR.Add(lDur).Format(time.RFC3339)), nil

			} else if lDurErr == nil && rDurErr == nil {
				// both sides durations
				return ajson.StringNode("sum", fmt.Sprintf("%vs", lDur.Seconds()+rDur.Seconds())), nil
			}

			return ajson.StringNode("sum", lNum+rNum), nil
		}

		lNum, rNum, err := _floats(left, right)
		if err != nil {
			return nil, err
		}
		return ajson.NumericNode("sum", lNum+rNum), nil
	})

	ajson.AddFunction("string", func(node *ajson.Node) (result *ajson.Node, err error) {
		var val string
		switch {
		case node.IsNumeric():
			val = fmt.Sprintf("%v", node.MustNumeric())
		case node.IsBool():
			val = "true"
			if !node.MustBool() {
				val = "false"
			}
		case node.IsObject():
			val = node.String()
		case node.IsString():
			val = node.MustString()
		default:
			val = "unknown"
		}
		return ajson.StringNode("string", val), nil
	})
}
