# Conclusão da rodada de issues #63–#89

Auditoria local em 2026-09-06, na branch `issues`. As 26 issues técnicas #63–#88 foram implementadas ou concluídas como estudos conforme seus escopos, validadas, enviadas ao remoto e fechadas individualmente. A #89 consolida essa rodada. Não houve publicação de release ou alteração da branch principal.

| Issue | Resultado | Commit principal |
|---|---|---|
| #63 | Asserção de timeout compatível com encerramento Unix | 3fc5aaf |
| #64 | Seleção transacional de fontes no LSP | 31eb0b1 |
| #65 | Recuperação persistente de rascunhos e abas | bfd8908 |
| #66 | Sugestões inequívocas e QuickFixes com spans precisos | de37d7f |
| #67 | [Assinaturas em chamadas incompletas](signature-help.md) | ff82c6d |
| #68 | [Testes de programas por manifesto](program-tests.md) | 7d36bed |
| #69 | [Formatação preservando comentários](formatting.md) em CLI/LSP/GUI | 214604e |
| #70 | Warning de bindings não lidos e seleção de regras | 68a9323 |
| #71 | Warning de ramos inalcançáveis por literais | abc4b0a |
| #72 | Warning e correção de autoatribuição preservando valor | 5444545 |
| #73 | Warning de isvoid redundante preservando avaliação | affdbb6 |
| #74 | [Extensão VS Code local](vscode-extension.md) com LSP e comandos | aaa696d |
| #75 | [Rename](rename.md) validado contra colisões/captura | 5e26646 |
| #76 | [Inlay hints opcionais](inlay-hints.md) | c623be3 |
| #77 | [Medição de latência do LSP](lsp-performance.md) | 4fb502c |
| #78 | Navegação direta no histórico retido e slider | b3250ec |
| #79 | [Grafos AST/CFG, navegação e exportação SVG/PNG](graphical-artifacts.md) | 47595fe |
| #80 | [Melhorias integradas e decisão sobre editor](editor-decision.md) | 756cdc8 |
| #81 | [Inspeção isolada de relatórios JSON](report-inspection.md) | 845b846 |
| #82 | Simplificações algébricas locais conservadoras | d3ee8a0 |
| #83 | Eliminação de computações comprovadamente descartáveis | 99e0877 |
| #84 | [Protótipo e decisão sobre inlining](inlining-experiment.md) | d3f361d |
| #85 | [Compilador e VM Go em worker Wasm no navegador](browser-wasm-experiment.md) | 7ab4510 |
| #86 | [Protótipo de backend direto Stack IR → Wasm](wasm-backend-experiment.md) | a9a5098 |
| #87 | [Protótipo RV32IM executado no RARS](riscv-backend-experiment.md) | 4e1070c |
| #88 | [Adaptador DAP local e integração VS Code](dap-debugger.md) | 1cefaf2 |

## Evidências de integração

- A suíte Go completa, `go vet ./...`, `bash scripts/run-lint.sh` e `go run ./cmd/cooler-doc-check` passaram ao concluir a implementação.
- Testes focados verificaram spans UTF-16, fontes multifile, semântica/equivalência das otimizações, configuração comum de warnings, segurança das edições e limites de recursos. Não foram executadas suítes de duração de horas.
- A GUI real no navegador foi exercitada para recuperação após reinício, formatação, importação de relatórios, histórico, grafos, exportação PNG e edição integrada com breakpoint/execução. Os documentos individuais distinguem testes DOM, acessibilidade e observações de desempenho.
- VS Code 1.136.1 instalou o VSIX local e validou LSP, QuickFix, Check/Run, seleção de fontes e sessões DAP reais. A depuração incluiu fonte auxiliar, frames/locals, passos, entrada/saída, pausa/retomada e encerramento. O teste race do DAP passou três vezes; testes do adaptador também passaram no Linux, inclusive rejeição de FIFO.
- A inspeção final de processos não encontrou executáveis de teste cooler-dap ou cooler-lsp após o host encerrar. A única entrada não rastreada preexistente é `AGENTS.md`, preservada sem inclusão nos commits.

## Escopo e limites mantidos

Nenhuma issue inteira foi adiada ou cancelada. Os estudos #84–#87 terminaram com protótipos reproduzíveis e decisões documentadas; isso não significa incorporar inlining nem backends completos como funcionalidades padrão. O worker Go/Wasm e o backend que gera programas Wasm são resultados distintos.

A #80 autoriza escolher um conjunto viável de melhorias: o textarea recebeu melhorias integradas, enquanto folding, múltiplos cursores e troca por CodeMirror continuam explicitamente fora do produto. O protótipo CodeMirror foi medido, mas não é apresentado como editor integrado. DAP não anuncia reverse execution, watches ou mutação de variáveis. Esses limites constam nas issues e documentos correspondentes.

WebViews nativas, VS Code nativo em Linux/macOS, leitores de tela externos, gestos reais de clipboard e IME físico não estavam disponíveis nas validações indicadas. Testes em navegador, eventos sintéticos e testes do adaptador Linux não substituem essas verificações de plataforma. A conclusão desta rodada não equivale a uma nova release.
