# godec — 開発者ドキュメント

**godec** は Go 製のメディア処理フレームワークです。  
FFmpeg に近い概念（デマックス → デコード → フィルタ → エンコード → マックス）を  
Go の型システムとジェネリクスを活かして実装しています。

## ドキュメント一覧

| ファイル | 内容 |
|---------|------|
| [architecture.md](./architecture.md) | システム全体のアーキテクチャ・設計思想 |
| [packages.md](./packages.md) | 各 Go モジュール / パッケージの役割と API |
| [data-model.md](./data-model.md) | ドメインモデル（Frame, Packet, StreamInfo など）の詳細 |
| [pipeline.md](./pipeline.md) | パイプライン構築・実行フロー |
| [plugin-system.md](./plugin-system.md) | プラグイン登録・解決の仕組みと実装ガイド |
| [plugins-builtin.md](./plugins-builtin.md) | 同梱プラグイン（WAV / PCM / filter-audio）の仕様 |
| [contributing.md](./contributing.md) | 開発環境・コーディング規約・テスト方針 |

## クイックリンク

- **モジュール構造** → [packages.md](./packages.md)
- **新しいプラグインを作る** → [plugin-system.md](./plugin-system.md)
- **データフロー図** → [architecture.md](./architecture.md)
- **テスト方法** → [contributing.md](./contributing.md)
