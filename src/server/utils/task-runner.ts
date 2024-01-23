import cluster from 'node:cluster'
import { dbUtils } from '../database/utils'
import consola from 'consola'

export interface Progress {
	label: string | null
	value: number
	total: number | null
}

export interface DefineTaskOptions {
	name: string
	displayName: string
	fn: (ctx: TaskContext) => Promise<any>
}

interface Task {
	id: string
	name: string
	displayName: string
	progress: Progress | null
	error: string | null
	startedAt: Date
	finishedAt: Date | null
	log: string
}

export class TaskRunner {
	history: Task[] = []
	tasks: Task[] = []

	constructor() {
		if (cluster.isPrimary) return
		process.on('message', msg => {
			if (!isTaskRunnerUpdate(msg)) return
			this.history = msg.history
			this.tasks = msg.tasks
		})
	}

	async run(def: DefineTaskOptions) {
		if (!cluster.isPrimary)
			throw new Error('Cannot run tasks on worker processes')

		const task = <Task>{
			id: dbUtils.createId(),
			name: def.name,
			displayName: def.displayName,
			progress: null,
			error: null,
			startedAt: new Date(),
			finishedAt: null,
			log: ''
		}
		const ctx = new TaskContext(task)

		this.tasks.push(task)
		taskRunner.updateWorkers()

		consola.info(`[task-runner] Task started: ${task.displayName} (${task.id})`)

		await def
			.fn(ctx)
			.catch(e => {
				task.error = e.message
			})
			.finally(() => {
				task.finishedAt = new Date()
				task.progress = null
				this.tasks = this.tasks.filter(t => t !== task)
				this.history.push(task)
				taskRunner.updateWorkers()
				consola.info(
					`[task-runner] Task ended: ${task.displayName} (${task.id})`,
					task
				)
			})
	}

	updateWorkers() {
		if (!cluster.isPrimary) return
		const workers = Object.values(cluster.workers!)
		const update = <TaskRunnerUpdate>{
			type: 'task-runner-update',
			tasks: this.tasks,
			history: this.history
		}
		for (const worker of workers) {
			worker?.send(update)
		}
	}
}

export const taskRunner = new TaskRunner()

export class TaskContext {
	constructor(public task: Task) {}

	setProgress(p: Progress) {
		this.task.progress = p
		taskRunner.updateWorkers()
	}

	clearProgress() {
		this.task.progress = null
		taskRunner.updateWorkers()
	}

	log(msg: string) {
		this.task.log += msg + '\n'
		taskRunner.updateWorkers()
	}
}

interface TaskRunnerUpdate {
	type: 'task-runner-update'
	tasks: Task[]
	history: Task[]
}

function isTaskRunnerUpdate(msg: any): msg is TaskRunnerUpdate {
	return msg.type === 'task-runner-update'
}
