import { useState, useEffect } from 'react';
import { FileText, Check } from '@phosphor-icons/react';

interface PersonaEditorProps {
	agentName: string;
	showToast: (msg: string, type?: 'success' | 'error') => void;
}

const PERSONA_FILES = [
	{ name: 'IDENTITY.md', desc: 'Define the agent\'s core persona, name, and identity.' },
	{ name: 'SOUL.md', desc: 'Define the agent\'s inner drive, motivations, and behavioral boundaries.' },
	{ name: 'CAPABILITIES.md', desc: 'State what the agent can and cannot do.' },
	{ name: 'BOOTSTRAP.md', desc: 'System setup instructions and environment guidelines.' },
	{ name: 'USER.md', desc: 'Instructions on how the agent should communicate with the user.' },
	{ name: 'AGENTS.md', desc: 'General description and constraints for multi-agent settings.' },
	{ name: 'MEMORY.md', desc: 'Durable curated core memory file (MEMORY.md).' },
];

export default function PersonaEditor({ agentName, showToast }: PersonaEditorProps) {
	const [selectedFile, setSelectedFile] = useState(PERSONA_FILES[0].name);
	const [content, setContent] = useState('');
	const [isLoading, setIsLoading] = useState(false);
	const [isSaving, setIsSaving] = useState(false);

	useEffect(() => {
		loadPersonaFile();
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [selectedFile, agentName]);

	const loadPersonaFile = async () => {
		setIsLoading(true);
		try {
			const res = await fetch(`/api/agents/${encodeURIComponent(agentName)}/persona/${selectedFile}`);
			if (res.ok) {
				setContent(await res.text());
			} else {
				showToast(`Failed to load ${selectedFile}`, 'error');
			}
		} catch {
			showToast(`Failed to load ${selectedFile}`, 'error');
		} finally {
			setIsLoading(false);
		}
	};

	const savePersonaFile = async () => {
		setIsSaving(true);
		try {
			const res = await fetch(`/api/agents/${encodeURIComponent(agentName)}/persona/${selectedFile}`, {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ content }),
			});
			if (res.ok) {
				showToast(`${selectedFile} saved successfully`, 'success');
			} else {
				const err = await res.json();
				showToast(err.error || `Failed to save ${selectedFile}`, 'error');
			}
		} catch {
			showToast(`Failed to save ${selectedFile}`, 'error');
		} finally {
			setIsSaving(false);
		}
	};

	return (
		<div className="card" style={{ display: 'grid', gridTemplateColumns: '240px 1fr', gap: '24px', padding: '24px', minHeight: '500px', cursor: 'default' }}>
			{/* Sidebar list of files */}
			<div style={{ borderRight: '1px solid var(--border-soft)', paddingRight: '20px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
				<h4 style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-bright)', margin: '0 0 12px 0', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
					Persona Files
				</h4>
				{PERSONA_FILES.map((file) => (
					<button
						key={file.name}
						onClick={() => setSelectedFile(file.name)}
						style={{
							display: 'flex',
							alignItems: 'center',
							gap: '10px',
							width: '100%',
							padding: '10px 12px',
							borderRadius: '6px',
							border: 'none',
							textAlign: 'left',
							background: selectedFile === file.name ? 'var(--bg-soft)' : 'transparent',
							color: selectedFile === file.name ? 'var(--text-bright)' : 'var(--text-muted)',
							cursor: 'pointer',
							fontSize: '13px',
							fontWeight: selectedFile === file.name ? 600 : 400,
							transition: 'all 0.15s ease',
						}}
						title={file.desc}
					>
						<FileText size={16} weight={selectedFile === file.name ? 'fill' : 'regular'} style={{ color: selectedFile === file.name ? 'var(--accent)' : 'inherit' }} />
						<span>{file.name}</span>
					</button>
				))}
			</div>

			{/* Main Editor */}
			<div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
				<div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
					<div>
						<h3 style={{ fontSize: '16px', fontWeight: 600, color: 'var(--text-bright)', margin: 0 }}>
							Editing: <code>{selectedFile}</code>
						</h3>
						<span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
							{PERSONA_FILES.find(f => f.name === selectedFile)?.desc}
						</span>
					</div>
					<button
						className="btn btn-primary btn-sm"
						onClick={savePersonaFile}
						disabled={isLoading || isSaving}
						style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
					>
						<Check size={16} weight="bold" />
						{isSaving ? 'Saving…' : 'Save File'}
					</button>
				</div>

				<div style={{ flexGrow: 1, position: 'relative', display: 'flex', flexDirection: 'column' }}>
					{isLoading ? (
						<div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '300px', color: 'var(--text-muted)' }}>
							<div className="spinner" style={{ marginRight: '10px' }} />
							<span>Loading file content…</span>
						</div>
					) : (
						<textarea
							value={content}
							onChange={(e) => setContent(e.target.value)}
							style={{
								width: '100%',
								flexGrow: 1,
								minHeight: '380px',
								fontFamily: 'monospace',
								fontSize: '13px',
								lineHeight: '1.6',
								backgroundColor: 'var(--bg-textarea)',
								color: 'var(--text)',
								border: '1px solid var(--border)',
								borderRadius: '6px',
								padding: '16px',
								resize: 'vertical',
								outline: 'none',
							}}
							placeholder={`# ${selectedFile}\n\nEnter markdown content here…`}
						/>
					)}
				</div>
			</div>
		</div>
	);
}
