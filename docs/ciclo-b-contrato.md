# Contrato executável do Ciclo B — 2026-09-14

Escopo desta retomada: repositório e serviços de documento. Entidade com Dono
preservada; migration, jobs, storage e HTTP ficam para os próximos ciclos.

- `DadosIngestao.Dono vo.Dono` substitui `UsuarioID`.
- Serviço público: preparar/validar ingestão, registrar, obter, listar do dono
  e registrar preview. As operações persistidas recebem solicitante `vo.Dono`.
- `DocumentoRepo`: Inserir, ObterPorID com solicitante, ListarPorDono e
  DefinirChavePreviewPDF com solicitante. Consulta e atualização escopadas pelo
  dono no adaptador futuro; serviço também verifica os donos retornados.
- Registro revalida entidade e exige documento novo sem preview ou CDM pronto.
- Preview deriva sua chave do ID autorizado. Terceiro e inexistente retornam
  o mesmo erro público; dono vazio é recusado antes de qualquer I/O.
- `processamento.ServicoInterno` contém IniciarAnalise, ConcluirAnalise e
  MarcarFalha. Pacote separado permite barrar dependência das rotas por imports.
- `DocumentoInternoRepo`: ObterPorIDInterno, AtualizarStatus e DefinirCDM;
  ambas as escritas recebem status anterior e novo para compare-and-set.
- Erros tipados existentes são preservados. `infra/errors` permanece como no
  projeto atual; a contradição com a regra de imports não será ampliada aqui.

Testes: isolamento entre sessões/usuários inclusive UUID coincidente, dono
vazio, repositório adversarial, registro forjado, preview derivado, transições
e conflitos concorrentes. Sem chamada externa ou dependência nova.
