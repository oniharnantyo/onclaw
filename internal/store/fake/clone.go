package fake

import "github.com/oniharnantyo/buddies/internal/store"

// clone helpers: every value crossing the fake's boundary is copied so a
// caller mutating a returned struct can never corrupt internal state.

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneAgent(in store.Agent) store.Agent {
	in.Tools = cloneStrings(in.Tools)
	in.Skills = cloneStrings(in.Skills)
	in.MCPServers = cloneStrings(in.MCPServers)
	return in
}

func cloneMCPServer(in store.MCPServer) store.MCPServer {
	in.Args = cloneStrings(in.Args)
	in.Env = cloneStringMap(in.Env)
	in.Headers = cloneStringMap(in.Headers)
	in.Auth.Scopes = cloneStrings(in.Auth.Scopes)
	return in
}

func cloneEvent(in store.SessionEvent) store.SessionEvent {
	in.Payload = cloneBytes(in.Payload)
	return in
}
