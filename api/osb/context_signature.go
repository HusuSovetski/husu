package osb

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/Peripli/service-manager/pkg/log"
	"github.com/Peripli/service-manager/pkg/web"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type ContextSignaturePlugin struct {
	CtxPrivateKey string
	CtxPublicKey  string
}

func NewCtxSignaturePlugin(publicKey, privateKey string) *ContextSignaturePlugin {
	return &ContextSignaturePlugin{
		CtxPrivateKey: privateKey,
		CtxPublicKey:  publicKey,
	}
}

func (s *ContextSignaturePlugin) Name() string {
	return "ContextSignaturePlugin"
}

func (s *ContextSignaturePlugin) Provision(req *web.Request, next web.Handler) (*web.Response, error) {
	return s.sign(req, next)
}

func (s *ContextSignaturePlugin) Bind(req *web.Request, next web.Handler) (*web.Response, error) {
	return s.sign(req, next)
}

func (s *ContextSignaturePlugin) sign(req *web.Request, next web.Handler) (*web.Response, error) {
	if s.CtxPrivateKey == "" || s.CtxPublicKey == "" {
		log.C(req.Context()).Infof("ctx private key or ctx public key is missing. signature will not be added to context")
		return next.Handle(req)
	}
	privateKey, err := parseRsaPrivateKey(req.Context(), s.CtxPrivateKey)
	if err != nil {
		return next.Handle(req)
	}

	ctx := gjson.GetBytes(req.Body, "context")
	if !ctx.Exists() {
		log.C(req.Context()).Error("could not find context on the request body")
		return next.Handle(req)
	}
	ctxStr := ctx.String()
	//unmarshal and marshal the context so the fields will be ordered lexicographically
	var ctxMap map[string]interface{}
	if err := json.Unmarshal([]byte(ctxStr), &ctxMap); err != nil {
		log.C(req.Context()).Errorf("failed to unmarshal context: %v", err)
		return next.Handle(req)
	}
	ctxByte, err := json.Marshal(ctxMap)
	if err != nil {
		log.C(req.Context()).Errorf("failed to marshal context: %v", err)
		return next.Handle(req)
	}
	ctxStr = string(ctxByte)

	signedCtx, err := getSignature(req.Context(), ctxStr, privateKey)
	if err != nil {
		return next.Handle(req)
	}

	reqBody, err := sjson.SetBytes(req.Body, "context.signature", signedCtx)
	if err != nil {
		log.C(req.Context()).Errorf("failed to set signature bytes to request body %v", err)
		return next.Handle(req)
	}
	req.Body = reqBody

	return next.Handle(req)
}

func getSignature(ctx context.Context, ctxStr string, key *rsa.PrivateKey) (string, error) {
	hashedCtx := sha256.Sum256([]byte(ctxStr))

	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashedCtx[:])
	if err != nil {
		log.C(ctx).Errorf("failed to encrypt context %v", err)
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func parseRsaPrivateKey(ctx context.Context, rsaPrivateKey string) (*rsa.PrivateKey, error) {
	key, err := base64.StdEncoding.DecodeString(rsaPrivateKey)
	if err != nil {
		log.C(ctx).Errorf("failed to base64 decode rsa private key: %v", err)
		return nil, err
	}
	block, _ := pem.Decode(key)
	if block == nil {
		log.C(ctx).Error("failed to pem decode rsa private key")
		return nil, fmt.Errorf("failed to pem decode context rsa private key")
	}
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		log.C(ctx).Errorf("fail to parse rsa key, %s", err.Error())
		return nil, err
	}

	return privateKey, nil
}
