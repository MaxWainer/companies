package model

type Field[T any] struct {
	Value T
	Set   bool
}

func Some[T any](v T) Field[T] { return Field[T]{Value: v, Set: true} }

type PatchParams struct {
	Name              Field[string]
	Description       Field[*string]
	AmountOfEmployees Field[int]
	Registered        Field[bool]
	Type              Field[Type]
}

func (p PatchParams) Validate() error {
	v := ValidationError{}
	if p.Name.Set {
		v.check("name", validateName(normalizeName(p.Name.Value)))
	}
	if p.Description.Set {
		v.check("description", validateDescription(p.Description.Value))
	}
	if p.AmountOfEmployees.Set {
		v.check("amount_of_employees", validateEmployees(p.AmountOfEmployees.Value))
	}
	if p.Type.Set {
		v.check("type", validateType(p.Type.Value))
	}
	return v.orNil()
}

func (p PatchParams) Apply(c *Company) []string {
	var changed []string
	if p.Name.Set {
		if name := normalizeName(p.Name.Value); name != c.Name {
			c.Name = name
			changed = append(changed, "name")
		}
	}
	if p.Description.Set && !equalPtr(p.Description.Value, c.Description) {
		c.Description = clonePtr(p.Description.Value)
		changed = append(changed, "description")
	}
	if p.AmountOfEmployees.Set && p.AmountOfEmployees.Value != c.AmountOfEmployees {
		c.AmountOfEmployees = p.AmountOfEmployees.Value
		changed = append(changed, "amount_of_employees")
	}
	if p.Registered.Set && p.Registered.Value != c.Registered {
		c.Registered = p.Registered.Value
		changed = append(changed, "registered")
	}
	if p.Type.Set && p.Type.Value != c.Type {
		c.Type = p.Type.Value
		changed = append(changed, "type")
	}
	return changed
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
