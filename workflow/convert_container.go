// SPDX-License-Identifier: MIT

package workflow

import (
	"bnema.dev/ermite-actions/template"
)

// convertContainer validates a job container or, when service is set, a
// service container. A token that contains an expression is not checked, as
// in the reference.
func (c *converter) convertContainer(t template.Token, service bool) {
	if template.HasExpression(t) {
		return
	}
	if _, ok := t.(*template.String); ok {
		return
	}
	var image template.Token
	c.handle(t, func() {
		m := asMap(t, "container item")
		for _, p := range m.Pairs {
			key := asString(p.Key, "container item key")
			v := p.Value
			switch key.Value {
			case "image":
				image = asString(v, "container image")
			case "credentials":
				c.convertCredentials(v)
			case "env":
				for _, e := range asMap(v, "container env").Pairs {
					asString(e.Key, "container env value")
				}
			case "ports":
				for _, it := range asSeq(v, "container ports").Items {
					asString(it, "container port")
				}
			case "volumes":
				for _, it := range asSeq(v, "container volumes").Items {
					asString(it, "container volume")
				}
			case "options":
				asString(v, "container options")
			case "entrypoint":
				if !service {
					c.ctx.Errorf(key, "Unexpected container item key: %s", key.Value)
					continue
				}
				asString(v, "container entrypoint")
			case "command":
				if !service {
					c.ctx.Errorf(key, "Unexpected container item key: %s", key.Value)
					continue
				}
				asString(v, "container command")
			default:
				c.ctx.Errorf(key, "Unexpected container item key: %s", key.Value)
			}
		}
	})
	if image == nil {
		if _, isMap := t.(*template.Mapping); isMap {
			c.ctx.ErrorAt(t, "Container image cannot be empty")
		}
	}
}

func (c *converter) convertCredentials(t template.Token) {
	for _, p := range asMap(t, "credentials").Pairs {
		key := asString(p.Key, "credentials item")
		switch key.Value {
		case "username":
			asString(p.Value, "credentials username")
		case "password":
			asString(p.Value, "credentials password")
		default:
			c.ctx.Errorf(key, "credentials key %s", key.Value)
		}
	}
}

// convertServices validates the services mapping of a job.
func (c *converter) convertServices(t template.Token) {
	for _, p := range asMap(t, "services").Pairs {
		asString(p.Key, "service key")
		c.convertContainer(p.Value, true)
	}
}
