package bpfparser

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Parser 语法分析器
type Parser struct {
	lexer   *Lexer
	curTok  Token
	peekTok Token
}

func NewParser(lexer *Lexer) *Parser {
	p := &Parser{lexer: lexer}
	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) nextToken() {
	p.curTok = p.peekTok
	p.peekTok = p.lexer.NextToken()
}

func (p *Parser) Parse() (*ASTNode, error) {
	return p.parseExpression()
}

func (p *Parser) parseExpression() (*ASTNode, error) {
	node, err := p.parseTerm()
	if err != nil {
		return nil, err
	}

	for p.curTok.Type == TokenOr {
		op := p.curTok
		p.nextToken()
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		node = &ASTNode{
			Type:     NodeBinaryOp,
			Operator: op.Type,
			Left:     node,
			Right:    right,
		}
	}

	return node, nil
}

func (p *Parser) parseTerm() (*ASTNode, error) {
	node, err := p.parseFactor()
	if err != nil {
		return nil, err
	}

	for p.curTok.Type == TokenAnd {
		op := p.curTok
		p.nextToken()
		right, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		node = &ASTNode{
			Type:     NodeBinaryOp,
			Operator: op.Type,
			Left:     node,
			Right:    right,
		}
	}

	return node, nil
}

func (p *Parser) parseFactor() (*ASTNode, error) {
	if p.curTok.Type == TokenNot {
		p.nextToken()
		node, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		return &ASTNode{
			Type:     NodeUnaryOp,
			Operator: TokenNot,
			Left:     node,
		}, nil
	}

	if p.curTok.Type == TokenLParen {
		p.nextToken()
		node, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if p.curTok.Type != TokenRParen {
			return nil, errors.New("expected ')'")
		}
		p.nextToken()
		return node, nil
	}

	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (*ASTNode, error) {
	switch p.curTok.Type {
	case TokenHost:
		return p.parseHost()
	case TokenNet:
		return p.parseNet()
	case TokenPort:
		return p.parsePort()
	case TokenPortRange:
		return p.parsePortRange()
	case TokenIdent:
		return p.parseProtocol()
	default:
		return nil, fmt.Errorf("unexpected token: %v", p.curTok)
	}
}

func (p *Parser) parseHost() (*ASTNode, error) {
	p.nextToken()
	if p.curTok.Type != TokenIdent && p.curTok.Type != TokenString {
		return nil, errors.New("expected host address")
	}

	ip := net.ParseIP(p.curTok.Value)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", p.curTok.Value)
	}

	node := &ASTNode{
		Type:    NodeHost,
		Address: ip,
		Value:   p.curTok.Value,
	}
	p.nextToken()
	return node, nil
}

func (p *Parser) parseNet() (*ASTNode, error) {
	p.nextToken()
	if p.curTok.Type != TokenIdent && p.curTok.Type != TokenString {
		return nil, errors.New("expected network address")
	}

	_, network, err := net.ParseCIDR(p.curTok.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid network: %s", p.curTok.Value)
	}

	node := &ASTNode{
		Type:    NodeNet,
		Network: network,
		Value:   p.curTok.Value,
	}
	p.nextToken()
	return node, nil
}

func (p *Parser) parsePort() (*ASTNode, error) {
	p.nextToken()
	if p.curTok.Type != TokenNumber {
		return nil, errors.New("expected port number")
	}

	port, err := strconv.Atoi(p.curTok.Value)
	if err != nil || port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port: %s", p.curTok.Value)
	}

	node := &ASTNode{
		Type:  NodePort,
		Port:  port,
		Value: p.curTok.Value,
	}
	p.nextToken()
	return node, nil
}

func (p *Parser) parsePortRange() (*ASTNode, error) {
	p.nextToken()
	if p.curTok.Type != TokenNumber {
		return nil, errors.New("expected port range")
	}

	parts := strings.Split(p.curTok.Value, "-")
	if len(parts) != 2 {
		return nil, errors.New("invalid port range format")
	}

	min, err := strconv.Atoi(parts[0])
	if err != nil || min < 0 || min > 65535 {
		return nil, fmt.Errorf("invalid min port: %s", parts[0])
	}

	max, err := strconv.Atoi(parts[1])
	if err != nil || max < 0 || max > 65535 || min > max {
		return nil, fmt.Errorf("invalid max port: %s", parts[1])
	}

	node := &ASTNode{
		Type:    NodePortRange,
		PortMin: min,
		PortMax: max,
		Value:   p.curTok.Value,
	}
	p.nextToken()
	return node, nil
}

func (p *Parser) parseProtocol() (*ASTNode, error) {
	protocol := strings.ToLower(p.curTok.Value)

	validProtocols := map[string]bool{
		"tcp":   true,
		"udp":   true,
		"icmp":  true,
		"ip":    true,
		"arp":   true,
		"ether": true,
	}

	if !validProtocols[protocol] {
		return nil, fmt.Errorf("unknown protocol: %s", protocol)
	}

	node := &ASTNode{
		Type:     NodeProtocol,
		Protocol: protocol,
		Value:    protocol,
	}
	p.nextToken()
	return node, nil
}
