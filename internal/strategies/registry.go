package strategies

// The registry map lives in strategy.go; the four builtin strategies register
// themselves through their subpackages' init() functions. This file is a
// placeholder so the registry lives in a clearly-named spot; the command
// layer imports the subpackages for their init-time side effects.
