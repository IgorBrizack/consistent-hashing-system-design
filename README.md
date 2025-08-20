# Consistent Hashing - System Design

[![Go](https://img.shields.io/badge/Go-1.21-blue)](https://golang.org/)
[![Redis](https://img.shields.io/badge/Redis-7.0-orange)](https://redis.io/)
[![Docker](https://img.shields.io/badge/Docker-24.0-blue)](https://www.docker.com/)
[![NGINX](https://img.shields.io/badge/NGINX-1.25-green)](https://nginx.org/)

---

## Sobre o Projeto

Olá! Meu nome é **Igor Brizack** e sou desenvolvedor de software.  
Este repositório demonstra **Consistent Hashing**, um padrão de distribuição de carga em sistemas distribuídos.  
O conhecimento aplicado foi adquirido do livro *System Design Interview*, focando em **escalabilidade**, **resiliência** e **tolerância a falhas**.

O projeto mostra como distribuir requests entre múltiplos servidores de forma equilibrada, garantindo **alta disponibilidade** e **redução de cache misses**.

### Desafios que Resolve

- Distribuição eficiente de carga entre servidores.
- Minimiza impacto de servidores que entram ou saem do cluster.
- Cache local eficiente com Redis como fonte de verdade.
- Multi-tenant isolation para separar dados de diferentes usuários.

---

## Tecnologias

- **Golang**: backend do serviço.
- **Redis**: cache distribuído centralizado.
- **NGINX**: balanceador de carga com consistent hashing.
- **Docker & Docker Compose**: containerização e orquestração do ambiente.

---

## Como Iniciar

1. Faça um fork do repositório e clone localmente:
```bash
git clone https://github.com/IgorBrizack/consistent-hashing-system-design.git
cd consistent-hashing-system-design


O serviço estará disponível em: http://localhost:80

| Método | Endpoint  | Descrição |
|--------|-----------|-----------|
| GET    | /health   | Verifica se o serviço está ativo |
| GET    | /kv?key=  | Busca o valor de uma chave (gera se não existir) |
| POST   | /kv       | Sobrescreve o valor de uma chave (JSON: {"key":"foo","value":"bar"}) |
| GET    | /whoami   | Retorna o servidor atendendo a requisição |
