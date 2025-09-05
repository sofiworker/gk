package bpfparser

import (
	"net"
)

type TokenType int

const (
	TokenEOF TokenType = iota
	TokenIdent
	TokenNumber
	TokenString
	TokenAnd
	TokenOr
	TokenNot
	TokenLParen
	TokenRParen
	TokenHost
	TokenNet
	TokenPort
	TokenPortRange
)

type Token struct {
	Type  TokenType
	Value string
	Pos   int
}

type NodeType int

const (
	NodeBinaryOp NodeType = iota
	NodeUnaryOp
	NodeProtocol
	NodeHost
	NodeNet
	NodePort
	NodePortRange
)

type ASTNode struct {
	Type     NodeType
	Operator TokenType
	Left     *ASTNode
	Right    *ASTNode
	Value    string
	Protocol string
	Address  net.IP
	Network  *net.IPNet
	Port     int
	PortMin  int
	PortMax  int
}

func ParseBPF(filter string) (*ASTNode, error) {
	lexer := NewLexer(filter)
	parser := NewParser(lexer)
	return parser.Parse()
}

func CompileToBPF(node *ASTNode) (string, error) {
	compiler := &BPFCompiler{}
	return compiler.Compile(node)
}
