package utils

//
//func NodeToMap(n *module.Node, data map[string]interface{}) map[string]interface{} {
//	ports := n.GetPorts()
//	handles := make([]interface{}, len(ports))
//	spin := 0
//
//	m := map[string]interface{}{
//		"type": "customNode",
//	}
//
//	if n.Position != nil {
//		m["position"] = map[string]interface{}{
//			"x": n.Position.X,
//			"y": n.Position.Y,
//		}
//		spin = int(n.Position.Spin)
//
//	} else {
//		m["position"] = map[string]interface{}{
//			"x": randFromRange(100, 300), // replace with faker
//			"y": randFromRange(100, 300),
//		}
//	}
//	for k, v := range ports {
//		var typ = "target"
//		if v.Source {
//			typ = "source"
//		}
//		ma := map[string]interface{}{
//			"id":                    v.PortName,
//			"type":                  typ,
//			"style":                 map[string]interface{}{},
//			"class":                 "",
//			"position":              v.Position,
//			"rotated_position":      (int(v.Position) + spin) % 4,
//			"label":                 v.Label,
//			"settings":              v.IsSettings,
//			"status":                v.Status,
//			"schema_default":        BytesToString(v.SchemaDefault),
//			"configuration_default": BytesToString(v.ConfigurationDefault),
//		}
//		for _, pc := range n.PortConfigs {
//			if pc.From != "" || pc.PortName != v.PortName {
//				continue
//			}
//			ma["schema"] = BytesToString(pc.Schema)
//			ma["schema_default"] = BytesToString(pc.SchemaDefault)
//			ma["configuration"] = BytesToString(pc.Configuration)
//			ma["configuration_default"] = BytesToString(pc.ConfigurationDefault)
//
//		}
//		handles[k] = ma
//	}
//	if data == nil {
//		data = map[string]interface{}{}
//	}
//	//data["component"] = n.Component.Name
//	data["handles"] = handles
//	data["runnable"] = n.Runnable
//	data["run"] = n.Run
//	data["spin"] = spin
//
//	m["data"] = data
//	return m
//}
//
//func randFromRange(min, max int) int {
//	rand.Seed(time.Now().UnixNano())
//	return rand.Intn(max-min+1) + min
//}
