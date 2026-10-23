package meta

import "github.com/arcgolabs/dbx/querydsl"

func blobTextDiffers(column querydsl.Operand, value string) querydsl.Predicate {
	return querydsl.Compare(blobTextOperand{column}, querydsl.OpNe, blobTextOperand{querydsl.Value(value)})
}

type blobTextOperand struct {
	inner querydsl.Operand
}

func (blobTextOperand) QueryExpression() {}

func (operand blobTextOperand) RenderOperand(state *querydsl.State) (string, error) {
	rendered, err := operand.inner.RenderOperand(state)
	if err != nil {
		return "", wrapError(err, "render blob content comparison")
	}
	if state.Dialect().Name() == metaDriverMySQL {
		// Object keys are byte-sensitive, including case and trailing spaces.
		return "CAST(" + rendered + " AS BINARY)", nil
	}
	return rendered, nil
}
