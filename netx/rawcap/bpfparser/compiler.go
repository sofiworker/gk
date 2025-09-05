package bpfparser

import "fmt"

type BPFCompiler struct{}

func (c *BPFCompiler) Compile(node *ASTNode) (string, error) {
	return c.compileNode(node)
}

func (c *BPFCompiler) compileNode(node *ASTNode) (string, error) {
	switch node.Type {
	case NodeBinaryOp:
		return c.compileBinaryOp(node)
	case NodeUnaryOp:
		return c.compileUnaryOp(node)
	case NodeProtocol:
		return c.compileProtocol(node)
	case NodeHost:
		return c.compileHost(node)
	case NodeNet:
		return c.compileNet(node)
	case NodePort:
		return c.compilePort(node)
	case NodePortRange:
		return c.compilePortRange(node)
	default:
		return "", fmt.Errorf("unknown node type: %v", node.Type)
	}
}

func (c *BPFCompiler) compileBinaryOp(node *ASTNode) (string, error) {
	left, err := c.compileNode(node.Left)
	if err != nil {
		return "", err
	}
	right, err := c.compileNode(node.Right)
	if err != nil {
		return "", err
	}

	switch node.Operator {
	case TokenAnd:
		return fmt.Sprintf("(%s and %s)", left, right), nil
	case TokenOr:
		return fmt.Sprintf("(%s or %s)", left, right), nil
	default:
		return "", fmt.Errorf("unknown operator: %v", node.Operator)
	}
}

func (c *BPFCompiler) compileUnaryOp(node *ASTNode) (string, error) {
	expr, err := c.compileNode(node.Left)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("not %s", expr), nil
}

func (c *BPFCompiler) compileProtocol(node *ASTNode) (string, error) {
	switch node.Protocol {
	case "tcp":
		return "tcp", nil
	case "udp":
		return "udp", nil
	case "icmp":
		return "icmp", nil
	case "ip":
		return "ip", nil
	case "arp":
		return "arp", nil
	case "ether":
		return "ether", nil
	default:
		return "", fmt.Errorf("unknown protocol: %s", node.Protocol)
	}
}

func (c *BPFCompiler) compileHost(node *ASTNode) (string, error) {
	return fmt.Sprintf("host %s", node.Value), nil
}

func (c *BPFCompiler) compileNet(node *ASTNode) (string, error) {
	return fmt.Sprintf("net %s", node.Value), nil
}

func (c *BPFCompiler) compilePort(node *ASTNode) (string, error) {
	return fmt.Sprintf("port %s", node.Value), nil
}

func (c *BPFCompiler) compilePortRange(node *ASTNode) (string, error) {
	return fmt.Sprintf("portrange %d-%d", node.PortMin, node.PortMax), nil
}
