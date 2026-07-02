package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

var (
	testConsoleCaptured func(msg string)
	testConsoleMu       sync.Mutex
)

func init() {
	Register("script", scriptFactory)
}

type scriptHandler struct {
	prg *goja.Program
}

func scriptFactory(cfgBytes []byte) (Handler, error) {
	var cfg ScriptConfig
	if len(cfgBytes) > 0 {
		if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
			return nil, fmt.Errorf("invalid script handler config: %w", err)
		}
	}
	if cfg.Script == "" {
		return nil, errors.New("script must not be empty")
	}

	prg, err := goja.Compile("script.js", cfg.Script, true)
	if err != nil {
		return nil, fmt.Errorf("compile javascript: %w", err)
	}

	return &scriptHandler{prg: prg}, nil
}

func (sh *scriptHandler) Run(ctx context.Context, payload Payload) (Decision, error) {
	vm := goja.New()

	// Enforce timeout using vm.Interrupt
	doneChan := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-doneChan:
		}
	}()
	defer close(doneChan)

	// Bind console
	console := vm.NewObject()
	_ = console.Set("log", func(call goja.FunctionCall) goja.Value {
		var parts []string
		for _, arg := range call.Arguments {
			parts = append(parts, arg.String())
		}
		msg := strings.Join(parts, " ")
		testConsoleMu.Lock()
		if testConsoleCaptured != nil {
			testConsoleCaptured(msg)
		}
		testConsoleMu.Unlock()
		return goja.Undefined()
	})
	_ = console.Set("error", func(call goja.FunctionCall) goja.Value {
		var parts []string
		for _, arg := range call.Arguments {
			parts = append(parts, arg.String())
		}
		msg := strings.Join(parts, " ")
		testConsoleMu.Lock()
		if testConsoleCaptured != nil {
			testConsoleCaptured(msg)
		}
		testConsoleMu.Unlock()
		return goja.Undefined()
	})
	_ = vm.Set("console", console)

	// Run compiled program to define function/globals
	_, err := vm.RunProgram(sh.prg)
	if err != nil {
		return DecisionBlock, fmt.Errorf("run script: %w", err)
	}

	// Retrieve handle function
	handleVal := vm.Get("handle")
	if handleVal == nil {
		return DecisionBlock, errors.New("script must define a 'handle(ctx)' function")
	}
	handleFn, ok := goja.AssertFunction(handleVal)
	if !ok {
		return DecisionBlock, errors.New("'handle' must be a function")
	}

	// Convert payload to JS value
	payloadVal := vm.ToValue(payload)

	// Invoke handle(ctx)
	resVal, err := handleFn(goja.Undefined(), payloadVal)
	if err != nil {
		return DecisionBlock, fmt.Errorf("execute handle: %w", err)
	}

	if resVal == nil || goja.IsUndefined(resVal) || goja.IsNull(resVal) {
		return DecisionAllow, nil
	}

	resObj := resVal.ToObject(vm)
	decisionVal := resObj.Get("decision")
	reasonVal := resObj.Get("reason")

	decision := DecisionAllow
	if decisionVal != nil && decisionVal.String() == "block" {
		decision = DecisionBlock
	}

	reason := ""
	if reasonVal != nil {
		reason = reasonVal.String()
	}

	if decision == DecisionBlock {
		if reason == "" {
			reason = "action blocked by script handler"
		}
		return DecisionBlock, errors.New(reason)
	}

	return DecisionAllow, nil
}
