import React, { useState, useEffect } from 'react';
import { useReactTable, getCoreRowModel, flexRender, createColumnHelper } from '@tanstack/react-table';
import {
	Plus,
	Trash,
	X,
	ToggleLeft,
	ToggleRight,
	Play,
	Plug,
	Globe,
	Terminal,
	Pencil,
} from '@phosphor-icons/react';
import Tooltip from './Tooltip';

export interface MCPServer {
	name: string;
	transport: string;
	command: string;
	args: string;
	env: string;
	url: string;
	enabled: boolean;
	created_at?: string;
	updated_at?: string;
}

interface MCPProps {
	showToast: (msg: string, type?: 'success' | 'error') => void;
	pinnedScope?: string;
}

export default function MCP({ showToast, pinnedScope }: MCPProps) {
	const [servers, setServers] = useState<MCPServer[]>([]);
	const [showModal, setShowModal] = useState(false);
	const [isEdit, setIsEdit] = useState(false);
	const [isProcessing, setIsProcessing] = useState(false);

	// Form Inputs
	const [name, setName] = useState('');
	const [transport, setTransport] = useState('stdio');
	const [command, setCommand] = useState('');
	const [args, setArgs] = useState('');
	const [env, setEnv] = useState('');
	const [envPairs, setEnvPairs] = useState<{ key: string; value: string }[]>([{ key: '', value: '' }]);
	const [url, setUrl] = useState('');
	const [enabled, setEnabled] = useState(true);

	// Validation / Test states
	const [errors, setErrors] = useState<Record<string, string>>({});
	const [testResult, setTestResult] = useState<{ tools: string[]; error?: string } | null>(null);
	const [isTesting, setIsTesting] = useState(false);
	const [touched, setTouched] = useState<Record<string, boolean>>({});

	useEffect(() => {
		loadServers();
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);

	// Real-time validations
	useEffect(() => {
		const nextErrors: Record<string, string> = {};

		if (!name.trim()) {
			nextErrors.name = 'Name is required';
		}

		if (transport === 'stdio') {
			if (!command.trim()) {
				nextErrors.command = 'Command is required';
			}
			if (args.trim()) {
				try {
					const parsed = JSON.parse(args.trim());
					if (!Array.isArray(parsed)) {
						nextErrors.args = 'Args must be a JSON array of strings';
					}
				} catch {
					nextErrors.args = 'Invalid JSON array';
				}
			}
			if (env.trim()) {
				try {
					const parsed = JSON.parse(env.trim());
					if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
						nextErrors.env = 'Env must be a JSON object of key-value pairs';
					}
				} catch {
					nextErrors.env = 'Invalid JSON object';
				}
			}
		} else {
			if (!url.trim()) {
				nextErrors.url = 'URL is required';
			} else {
				try {
					new URL(url.trim());
				} catch {
					nextErrors.url = 'Invalid URL format';
				}
			}
		}

		setErrors(nextErrors);
	}, [name, transport, command, args, env, url]);

	// Reset test results when form inputs change
	useEffect(() => {
		setTestResult(null);
	}, [name, transport, command, args, env, url, enabled]);

	const loadServers = async () => {
		try {
			const fetchUrl = pinnedScope && pinnedScope !== 'global'
				? `/api/agents/${pinnedScope}/mcp`
				: '/api/mcp';
			const res = await fetch(fetchUrl);
			if (res.ok) {
				setServers(await res.json());
			} else {
				showToast('Failed to load MCP servers', 'error');
			}
		} catch {
			showToast('Failed to load MCP servers', 'error');
		}
	};

	const resetForm = () => {
		setName('');
		setTransport('stdio');
		setCommand('');
		setArgs('');
		setEnv('');
		setEnvPairs([{ key: '', value: '' }]);
		setUrl('');
		setEnabled(true);
		setErrors({});
		setTestResult(null);
		setIsTesting(false);
		setTouched({});
	};

	const handleAddClick = () => {
		setIsEdit(false);
		resetForm();
		setShowModal(true);
	};

	const handleEditClick = (srv: MCPServer) => {
		setIsEdit(true);
		setName(srv.name);
		setTransport(srv.transport);
		setCommand(srv.command || '');
		setArgs(srv.args || '');
		setEnv(srv.env || '');
		let parsedEnvPairs = [{ key: '', value: '' }];
		if (srv.env) {
			try {
				const parsed = JSON.parse(srv.env);
				if (typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)) {
					parsedEnvPairs = Object.entries(parsed).map(([k, v]) => ({ key: k, value: String(v) }));
				}
			} catch {
				// ignore
			}
		}
		if (parsedEnvPairs.length === 0) {
			parsedEnvPairs = [{ key: '', value: '' }];
		}
		setEnvPairs(parsedEnvPairs);
		setUrl(srv.url || '');
		setEnabled(srv.enabled);
		setErrors({});
		setTestResult(null);
		setIsTesting(false);
		setTouched({});
		setShowModal(true);
	};

	const toggleEnabled = async (srv: MCPServer) => {
		try {
			if (pinnedScope && pinnedScope !== 'global') {
				const res = await fetch(`/api/agents/${pinnedScope}/mcp`, {
					method: 'PUT',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ server_name: srv.name, enabled: !srv.enabled }),
				});
				if (res.ok) {
					showToast(`MCP server "${srv.name}" ${!srv.enabled ? 'enabled' : 'disabled'} for agent successfully`, 'success');
					loadServers();
				} else {
					const err = await res.json();
					showToast(err.error || 'Failed to toggle MCP server for agent', 'error');
				}
				return;
			}
			const res = await fetch(`/api/mcp/${encodeURIComponent(srv.name)}/toggle`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ enabled: !srv.enabled }),
			});
			if (res.ok) {
				showToast(`Server "${srv.name}" ${!srv.enabled ? 'enabled' : 'disabled'} successfully`, 'success');
				loadServers();
			} else {
				const err = await res.json();
				showToast(err.error || 'Failed to toggle server', 'error');
			}
		} catch {
			showToast('Failed to toggle server', 'error');
		}
	};

	const handleDelete = async (srv: MCPServer) => {
		if (!confirm(`Are you sure you want to delete MCP server "${srv.name}"?`)) {
			return;
		}
		try {
			const res = await fetch(`/api/mcp/${encodeURIComponent(srv.name)}`, {
				method: 'DELETE',
			});
			if (res.ok) {
				showToast(`Server "${srv.name}" deleted successfully`, 'success');
				loadServers();
			} else {
				const err = await res.json();
				showToast(err.error || 'Failed to delete server', 'error');
			}
		} catch {
			showToast('Failed to delete server', 'error');
		}
	};

	const handleTest = async () => {
		setTouched({
			name: true,
			command: true,
			args: true,
			env: true,
			url: true,
		});

		if (Object.keys(errors).length > 0) {
			showToast('Please fix validation errors before testing', 'error');
			return;
		}

		setIsTesting(true);
		setTestResult(null);

		const payload = {
			name: name.trim(),
			transport,
			command: transport === 'stdio' ? command.trim() : '',
			args: transport === 'stdio' ? args.trim() : '',
			env: transport === 'stdio' ? env.trim() : '',
			url: transport !== 'stdio' ? url.trim() : '',
			enabled,
		};

		try {
			const res = await fetch('/api/mcp/test', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(payload),
			});
			const data = await res.json();
			if (res.ok && !data.error) {
				setTestResult({ tools: data.tools || [] });
				showToast('Connection test successful', 'success');
			} else {
				setTestResult({ tools: [], error: data.error || 'Connection failed' });
				showToast('Connection test failed', 'error');
			}
		} catch (err: any) {
			setTestResult({ tools: [], error: err.message || 'Network error' });
			showToast('Connection test failed', 'error');
		} finally {
			setIsTesting(false);
		}
	};

	const handleSubmit = async (e: React.FormEvent) => {
		e.preventDefault();
		setTouched({
			name: true,
			command: true,
			args: true,
			env: true,
			url: true,
		});

		if (Object.keys(errors).length > 0) {
			showToast('Please fix validation errors before saving', 'error');
			return;
		}

		setIsProcessing(true);
		const payload = {
			name: name.trim(),
			transport,
			command: transport === 'stdio' ? command.trim() : '',
			args: transport === 'stdio' ? args.trim() : '',
			env: transport === 'stdio' ? env.trim() : '',
			url: transport !== 'stdio' ? url.trim() : '',
			enabled,
		};

		const urlEndpoint = isEdit ? `/api/mcp/${encodeURIComponent(name.trim())}` : '/api/mcp';
		const method = isEdit ? 'PUT' : 'POST';

		try {
			const res = await fetch(urlEndpoint, {
				method,
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(payload),
			});
			if (res.ok) {
				showToast(`Server "${name.trim()}" saved successfully`, 'success');
				setShowModal(false);
				loadServers();
			} else {
				const data = await res.json();
				showToast(data.error || 'Failed to save server', 'error');
			}
		} catch {
			showToast('Failed to save server', 'error');
		} finally {
			setIsProcessing(false);
		}
	};

	// TanStack Table
	const columnHelper = createColumnHelper<MCPServer>();
	const columns = [
		columnHelper.accessor('name', {
			header: 'Name',
			cell: (info) => (
				<div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontWeight: 600 }}>
					<Plug size={16} weight="duotone" style={{ color: 'var(--accent-color)' }} />
					<span>{info.getValue()}</span>
				</div>
			),
		}),
		columnHelper.accessor('transport', {
			header: 'Transport',
			cell: (info) => (
				<span className="badge badge-secondary" style={{ textTransform: 'uppercase', fontSize: '11px' }}>
					{info.getValue()}
				</span>
			),
		}),
		columnHelper.display({
			id: 'details',
			header: 'Config / URL',
			cell: (info) => {
				const srv = info.row.original;
				if (srv.transport === 'stdio') {
					return (
						<div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '12px', color: 'var(--text-muted)' }}>
							<Terminal size={14} />
							<span style={{ fontFamily: 'monospace' }}>{srv.command}</span>
						</div>
					);
				}
				return (
					<div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '12px', color: 'var(--text-muted)' }}>
						<Globe size={14} />
						<span style={{ fontFamily: 'monospace', maxWidth: '250px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={srv.url}>
							{srv.url}
						</span>
					</div>
				);
			},
		}),
		columnHelper.accessor('enabled', {
			header: 'Status',
			cell: (info) => {
				const srv = info.row.original;
				return (
					<button
						onClick={() => toggleEnabled(srv)}
						style={{
							background: 'none',
							border: 'none',
							cursor: 'pointer',
							color: srv.enabled ? 'var(--accent-color)' : 'var(--text-muted)',
							padding: 0,
							display: 'flex',
							alignItems: 'center',
						}}
						title={srv.enabled ? 'Click to disable' : 'Click to enable'}
					>
						{srv.enabled ? <ToggleRight size={28} weight="fill" /> : <ToggleLeft size={28} />}
					</button>
				);
			},
		}),
	];

	if (!pinnedScope || pinnedScope === 'global') {
		columns.push(
			columnHelper.display({
				id: 'actions',
				header: 'Actions',
				cell: (info) => {
					const srv = info.row.original;
					return (
						<div style={{ display: 'flex', gap: '6px' }}>
							<button
								className="btn btn-secondary btn-sm"
								onClick={() => handleEditClick(srv)}
								title="Edit config"
								style={{ padding: '4px' }}
							>
								<Pencil size={15} />
							</button>
							<button
								className="btn btn-danger btn-sm"
								onClick={() => handleDelete(srv)}
								title="Delete server"
								style={{ padding: '4px' }}
							>
								<Trash size={15} />
							</button>
						</div>
					);
				},
			})
		);
	}

	const table = useReactTable({
		data: servers || [],
		columns,
		getCoreRowModel: getCoreRowModel(),
	});

	return (
		<div className="page-container">
			{/* Toolbar */}
			<div className="page-toolbar">
				<div className="page-toolbar-left">
					<span className="badge badge-inactive">
						{servers.length} server{servers.length !== 1 ? 's' : ''}
					</span>
				</div>
				{!pinnedScope && (
					<button className="btn btn-primary btn-sm" onClick={handleAddClick}>
						<Plus size={14} weight="bold" aria-hidden /> Add Server
					</button>
				)}
			</div>

			{/* Content */}
			<div style={{ padding: '20px 24px', overflow: 'auto', flexGrow: 1 }}>
				{servers.length === 0 ? (
					<div className="empty-state">
						<div className="empty-state-icon" aria-hidden="true">
							<Plug size={40} weight="duotone" />
						</div>
						<h3>No MCP servers configured</h3>
						<p>
							{pinnedScope && pinnedScope !== 'global'
								? 'No MCP servers are enabled or configured for this agent.'
								: 'Add a local stdio command server or a remote http/sse server to get started.'}
						</p>
						{!pinnedScope && (
							<button className="btn btn-primary btn-sm" style={{ marginTop: '8px' }} onClick={handleAddClick}>
								<Plus size={14} weight="bold" aria-hidden /> Configure first server
							</button>
						)}
					</div>
				) : (
					<div style={{ overflowX: 'auto', border: '1px solid var(--border)', borderRadius: 'var(--radius-md)' }}>
						<table style={{ width: '100%', borderCollapse: 'collapse', backgroundColor: 'var(--card)' }}>
							<thead>
								{table.getHeaderGroups().map(headerGroup => (
									<tr key={headerGroup.id} style={{ borderBottom: '1px solid var(--border)', background: 'rgba(0,0,0,0.15)' }}>
										{headerGroup.headers.map(header => (
											<th key={header.id} scope="col" style={{ padding: '11px 16px', fontSize: '11px', fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', textAlign: 'left' }}>
												{flexRender(header.column.columnDef.header, header.getContext())}
											</th>
										))}
									</tr>
								))}
							</thead>
							<tbody>
								{table.getRowModel().rows.map(row => (
									<tr key={row.id} style={{ borderBottom: '1px solid var(--border)' }} className="table-row-hover">
										{row.getVisibleCells().map(cell => (
											<td key={cell.id} style={{ padding: '12px 16px', fontSize: '13px' }}>
												{flexRender(cell.column.columnDef.cell, cell.getContext())}
											</td>
										))}
									</tr>
								))}
							</tbody>
						</table>
					</div>
				)}
			</div>

			{/* Add/Edit Modal */}
			{showModal && (
				<div
					className="modal-overlay"
					onClick={(e) => { if (e.target === e.currentTarget) setShowModal(false); }}
					role="dialog"
					aria-modal="true"
					aria-labelledby="modal-title"
				>
					<form className="modal-content" onSubmit={handleSubmit} style={{ maxWidth: '600px' }} noValidate>
						<div className="modal-header">
							<h2 id="modal-title" className="modal-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
								<Plus size={20} weight="bold" /> {isEdit ? 'Edit MCP Server' : 'Configure New MCP Server'}
							</h2>
							<button type="button" className="modal-close" onClick={() => setShowModal(false)} aria-label="Close dialog">
								<X size={18} weight="bold" />
							</button>
						</div>

						<div className="form-group">
							<label className="form-label" htmlFor="mcp-name">
								Server Name *
								<Tooltip content="Unique identifier for this MCP server." position="bottom" align="left" />
							</label>
							<input
								id="mcp-name"
								type="text"
								className={`form-input ${(touched.name && errors.name) ? 'is-invalid' : ''}`}
								value={name}
								onChange={e => {
									setName(e.target.value);
									setTouched(prev => ({ ...prev, name: true }));
								}}
								placeholder="e.g. filesystem"
								disabled={isEdit}
								required
							/>
							{touched.name && errors.name && (
								<span className="form-error">{errors.name}</span>
							)}
						</div>

						<div className="form-group">
							<label className="form-label" htmlFor="mcp-transport">
								Transport Type *
								<Tooltip content="Choose how the agent connects to the MCP server. stdio for local processes; http or sse for remote network services." position="bottom" align="left" />
							</label>
							<select
								id="mcp-transport"
								className="form-select"
								value={transport}
								onChange={e => setTransport(e.target.value)}
							>
								<option value="stdio">stdio (Local Subprocess)</option>
								<option value="http">http (Remote Streamable HTTP)</option>
								<option value="sse">sse (Remote SSE Endpoint)</option>
							</select>
						</div>

						{transport === 'stdio' ? (
							<>
								<div className="form-group">
									<label className="form-label" htmlFor="mcp-command">
										Command *
										<Tooltip content="Command or binary to run (e.g. node, python, npx)." position="bottom" align="left" />
									</label>
									<input
										id="mcp-command"
										type="text"
										className={`form-input ${(touched.command && errors.command) ? 'is-invalid' : ''}`}
										value={command}
										onChange={e => {
											setCommand(e.target.value);
											setTouched(prev => ({ ...prev, command: true }));
										}}
										placeholder="e.g. npx"
										required={transport === 'stdio'}
									/>
									{touched.command && errors.command && (
										<span className="form-error">{errors.command}</span>
									)}
								</div>

								<div className="form-group">
									<label className="form-label" htmlFor="mcp-args">
										Arguments (JSON Array)
										<Tooltip content="Command line arguments as a JSON array (e.g. ['-y', '@modelcontextprotocol/server-filesystem', '/Users'])." position="bottom" align="left" />
									</label>
									<input
										id="mcp-args"
										type="text"
										className={`form-input ${(touched.args && errors.args) ? 'is-invalid' : ''}`}
										value={args}
										onChange={e => {
											setArgs(e.target.value);
											setTouched(prev => ({ ...prev, args: true }));
										}}
										placeholder='e.g. ["-y", "@modelcontextprotocol/server-postgres", "postgresql://..."]'
									/>
									{touched.args && errors.args && (
										<span className="form-error">{errors.args}</span>
									)}
								</div>

								<div className="form-group">
									<label className="form-label">
										Environment Variables
										<Tooltip content="Configure environment variables for the MCP subprocess." position="bottom" align="left" />
									</label>
									<div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
										{envPairs.map((pair, index) => (
											<div key={index} style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
												<input
													type="text"
													className="form-input"
													style={{ flex: 1, fontFamily: 'monospace', fontSize: '13px' }}
													placeholder="Variable Name (e.g. API_KEY)"
													value={pair.key}
													onChange={(e) => {
														const newPairs = [...envPairs];
														newPairs[index].key = e.target.value;
														setEnvPairs(newPairs);
														
														// update raw env state
														const obj: Record<string, string> = {};
														for (const p of newPairs) {
															if (p.key.trim()) {
																obj[p.key.trim()] = p.value;
															}
														}
														setEnv(Object.keys(obj).length > 0 ? JSON.stringify(obj) : '');
														setTouched(prev => ({ ...prev, env: true }));
													}}
												/>
												<input
													type="text"
													className="form-input"
													style={{ flex: 2, fontFamily: 'monospace', fontSize: '13px' }}
													placeholder="Value"
													value={pair.value}
													onChange={(e) => {
														const newPairs = [...envPairs];
														newPairs[index].value = e.target.value;
														setEnvPairs(newPairs);
														
														// update raw env state
														const obj: Record<string, string> = {};
														for (const p of newPairs) {
															if (p.key.trim()) {
																obj[p.key.trim()] = p.value;
															}
														}
														setEnv(Object.keys(obj).length > 0 ? JSON.stringify(obj) : '');
														setTouched(prev => ({ ...prev, env: true }));
													}}
												/>
												<button
													type="button"
													className="btn btn-secondary"
													style={{ padding: '8px', display: 'flex', alignItems: 'center', justifyContent: 'center', borderColor: 'var(--border)' }}
													onClick={() => {
														const newPairs = envPairs.filter((_, idx) => idx !== index);
														const finalPairs = newPairs.length > 0 ? newPairs : [{ key: '', value: '' }];
														setEnvPairs(finalPairs);
														
														// update raw env state
														const obj: Record<string, string> = {};
														for (const p of finalPairs) {
															if (p.key.trim()) {
																obj[p.key.trim()] = p.value;
															}
														}
														setEnv(Object.keys(obj).length > 0 ? JSON.stringify(obj) : '');
														setTouched(prev => ({ ...prev, env: true }));
													}}
													title="Remove variable"
												>
													<Trash size={15} />
												</button>
											</div>
										))}

										<button
											type="button"
											className="btn btn-secondary btn-sm"
											style={{ display: 'flex', alignItems: 'center', gap: '4px', alignSelf: 'flex-start', marginTop: '4px' }}
											onClick={() => {
												setEnvPairs([...envPairs, { key: '', value: '' }]);
											}}
										>
											<Plus size={14} /> Add Variable
										</button>
									</div>
									{touched.env && errors.env && (
										<span className="form-error" style={{ display: 'block', marginTop: '4px' }}>{errors.env}</span>
									)}
								</div>
							</>
						) : (
							<div className="form-group">
								<label className="form-label" htmlFor="mcp-url">
									URL *
									<Tooltip content="The endpoint URL for the remote HTTP/SSE server." position="bottom" align="left" />
								</label>
								<input
									id="mcp-url"
									type="text"
									className={`form-input ${(touched.url && errors.url) ? 'is-invalid' : ''}`}
									value={url}
									onChange={e => {
										setUrl(e.target.value);
										setTouched(prev => ({ ...prev, url: true }));
									}}
									placeholder="e.g. http://localhost:3001/sse"
									required={transport !== 'stdio'}
								/>
								{touched.url && errors.url && (
									<span className="form-error">{errors.url}</span>
								)}
							</div>
						)}

						<div className="form-group" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', gap: '8px', marginBottom: 0 }}>
							<input
								id="mcp-enabled"
								type="checkbox"
								checked={enabled}
								onChange={e => setEnabled(e.target.checked)}
								style={{ width: 'auto' }}
							/>
							<label htmlFor="mcp-enabled" style={{ margin: 0, fontWeight: 500, fontSize: '14px', cursor: 'pointer' }}>
								Enabled
							</label>
						</div>

						{testResult && (
							<div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
								<div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
									<span style={{ fontSize: '13px', fontWeight: 500 }}>Connection Test:</span>
									<span className={`badge ${testResult.error ? 'badge-inactive' : 'badge-primary'}`}>
										{testResult.error ? 'Failed' : 'Success'}
									</span>
								</div>
								{testResult.error ? (
									<div style={{ fontSize: '12px', color: 'var(--error)', backgroundColor: 'var(--error-muted)', border: '1px solid var(--error-border)', borderRadius: 'var(--radius-sm)', padding: '8px 12px', fontFamily: 'monospace' }}>
										{testResult.error}
									</div>
								) : (
									<div style={{ fontSize: '13px', color: 'var(--text-muted)' }}>
										Discovered {testResult.tools.length} tool(s):
										<div style={{ marginTop: '4px', display: 'flex', flexWrap: 'wrap', gap: '4px' }}>
											{testResult.tools.map(t => (
												<span key={t} className="badge badge-secondary" style={{ fontFamily: 'monospace', fontSize: '11px' }}>{t}</span>
											))}
											{testResult.tools.length === 0 && <span style={{ fontStyle: 'italic' }}>None</span>}
										</div>
									</div>
								)}
							</div>
						)}

						<div className="modal-footer">
							<button type="button" className="btn btn-secondary btn-sm" onClick={() => setShowModal(false)}>
								Cancel
							</button>
							<button
								type="button"
								className="btn btn-secondary btn-sm"
								onClick={handleTest}
								disabled={isProcessing || isTesting}
								style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
							>
								<Play size={14} />
								{isTesting ? 'Testing...' : 'Test Connection'}
							</button>
							<button
								type="submit"
								className="btn btn-primary btn-sm"
								disabled={isProcessing || isTesting}
							>
								{isProcessing ? 'Saving...' : 'Save Server'}
							</button>
						</div>
					</form>
				</div>
			)}
		</div>
	);
}
