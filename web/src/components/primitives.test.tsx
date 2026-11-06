import { fireEvent, render, screen } from '@testing-library/react'
import axe from 'axe-core'
import { expect, test, vi } from 'vitest'
import { ContentNotStored } from './ContentNotStored'
import { BooleanFilter, FilterMenu, NumberFilter, TextListFilter } from './FilterMenu'
import { TimeRangePicker } from './TimeRangePicker'

test('time range picker reports presets, default as empty', () => {
  const onChange = vi.fn()
  render(<TimeRangePicker value={{ range: '7d' }} onChange={onChange} />)
  expect(screen.getByRole('button', { name: '7d' }).getAttribute('aria-pressed')).toBe('true')
  fireEvent.click(screen.getByRole('button', { name: '1h' }))
  expect(onChange).toHaveBeenLastCalledWith({ range: '1h' })
  fireEvent.click(screen.getByRole('button', { name: '24h' }))
  expect(onChange).toHaveBeenLastCalledWith({})
})

test('time range picker reports an absolute bound', () => {
  const onChange = vi.fn()
  render(<TimeRangePicker value={{}} onChange={onChange} />)
  fireEvent.change(screen.getByLabelText('From'), { target: { value: '2026-01-01T10:00' } })
  const arg = onChange.mock.calls[0][0] as { from: string }
  expect(Date.parse(arg.from)).toBe(new Date('2026-01-01T10:00').getTime())
})

test('filter menu fields add and remove values', () => {
  const onList = vi.fn()
  const onBool = vi.fn()
  const onNum = vi.fn()
  render(
    <FilterMenu activeCount={2} onClear={() => {}}>
      <TextListFilter label="Model" values={['a']} onChange={onList} />
      <BooleanFilter label="Streamed" value={undefined} onChange={onBool} />
      <NumberFilter label="Latency at least" value={undefined} onChange={onNum} />
    </FilterMenu>,
  )
  expect(screen.getByRole('button', { name: 'Filters (2)' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Filters (2)' }))
  fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'b' } })
  fireEvent.keyDown(screen.getByLabelText('Model'), { key: 'Enter' })
  expect(onList).toHaveBeenLastCalledWith(['a', 'b'])
  fireEvent.click(screen.getByRole('button', { name: 'Remove Model a' }))
  expect(onList).toHaveBeenLastCalledWith(undefined)
  fireEvent.change(screen.getByLabelText('Streamed'), { target: { value: 'false' } })
  expect(onBool).toHaveBeenLastCalledWith(false)
  fireEvent.change(screen.getByLabelText('Latency at least'), { target: { value: '300' } })
  expect(onNum).toHaveBeenLastCalledWith(300)
})

test('primitives have no accessibility violations', async () => {
  const { container } = render(
    <div>
      <TimeRangePicker value={{}} onChange={() => {}} />
      <FilterMenu activeCount={1} onClear={() => {}}>
        <TextListFilter label="Model" values={['a']} onChange={() => {}} />
        <BooleanFilter label="Streamed" value={true} onChange={() => {}} />
        <NumberFilter label="Latency at least" value={5} onChange={() => {}} />
      </FilterMenu>
      <ContentNotStored />
    </div>,
  )
  const results = await axe.run(container, { rules: { 'color-contrast': { enabled: false } } })
  expect(results.violations.map((v) => `${v.id}: ${v.help}`)).toEqual([])
})
