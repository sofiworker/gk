package bpfparser

import (
	"strings"
	"unicode"
)

// Lexer 词法分析器
type Lexer struct {
	input   string
	pos     int
	readPos int
	ch      rune
}

func NewLexer(input string) *Lexer {
	l := &Lexer{input: input}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPos >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = rune(l.input[l.readPos])
	}
	l.pos = l.readPos
	l.readPos++
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	if l.ch == 0 {
		return Token{Type: TokenEOF, Pos: l.pos}
	}

	switch l.ch {
	case '(':
		tok := Token{Type: TokenLParen, Value: string(l.ch), Pos: l.pos}
		l.readChar()
		return tok
	case ')':
		tok := Token{Type: TokenRParen, Value: string(l.ch), Pos: l.pos}
		l.readChar()
		return tok
	case '!':
		tok := Token{Type: TokenNot, Value: string(l.ch), Pos: l.pos}
		l.readChar()
		return tok
	}

	if isLetter(l.ch) {
		ident := l.readIdentifier()
		tokenType := l.lookupIdent(ident)
		return Token{Type: tokenType, Value: ident, Pos: l.pos}
	}

	if isDigit(l.ch) {
		number := l.readNumber()
		return Token{Type: TokenNumber, Value: number, Pos: l.pos}
	}

	if l.ch == '"' {
		str := l.readString()
		return Token{Type: TokenString, Value: str, Pos: l.pos}
	}

	tok := Token{Type: TokenIdent, Value: string(l.ch), Pos: l.pos}
	l.readChar()
	return tok
}

func (l *Lexer) readIdentifier() string {
	pos := l.pos
	for isLetter(l.ch) || isDigit(l.ch) || l.ch == '-' {
		l.readChar()
	}
	return l.input[pos:l.pos]
}

func (l *Lexer) readNumber() string {
	pos := l.pos
	for isDigit(l.ch) {
		l.readChar()
	}
	return l.input[pos:l.pos]
}

func (l *Lexer) readString() string {
	l.readChar() // 跳过第一个引号
	pos := l.pos
	for l.ch != '"' && l.ch != 0 {
		l.readChar()
	}
	str := l.input[pos:l.pos]
	l.readChar() // 跳过最后一个引号
	return str
}

func (l *Lexer) skipWhitespace() {
	for unicode.IsSpace(l.ch) {
		l.readChar()
	}
}

func (l *Lexer) lookupIdent(ident string) TokenType {
	switch strings.ToLower(ident) {
	case "and", "&&":
		return TokenAnd
	case "or", "||":
		return TokenOr
	case "not", "!":
		return TokenNot
	case "host":
		return TokenHost
	case "net":
		return TokenNet
	case "port":
		return TokenPort
	case "portrange":
		return TokenPortRange
	case "tcp", "udp", "icmp", "ip", "arp", "ether":
		return TokenIdent
	default:
		return TokenIdent
	}
}
