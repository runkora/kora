import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { TimeField } from './time-field';

function setup(initial = '06:00') {
  const onChange = vi.fn();
  // The parent owns the value, as the schedule builder does.
  function Parent() {
    const [value, setValue] = useState(initial);
    return (
      <TimeField
        id="t"
        value={value}
        onChange={(v) => {
          onChange(v);
          setValue(v);
        }}
      />
    );
  }
  render(<Parent />);
  return {
    onChange,
    hour: screen.getByLabelText('Hour'),
    minute: screen.getByLabelText('Minute'),
  };
}

describe('TimeField', () => {
  it('shows the hour and minute of a 24-hour time', () => {
    const { hour, minute } = setup('18:05');
    expect(hour).toHaveValue('18');
    expect(minute).toHaveValue('05');
  });

  it('emits a zero-padded HH:MM as each part is typed', () => {
    const { hour, minute, onChange } = setup('06:00');
    fireEvent.change(hour, { target: { value: '7' } });
    expect(onChange).toHaveBeenLastCalledWith('07:00');
    fireEvent.change(minute, { target: { value: '30' } });
    expect(onChange).toHaveBeenLastCalledWith('07:30');
  });

  it('holds each part to its range and to digits', () => {
    const { hour, minute, onChange } = setup('06:00');
    fireEvent.change(hour, { target: { value: '99' } });
    expect(onChange).toHaveBeenLastCalledWith('23:00');
    fireEvent.change(minute, { target: { value: '7x' } });
    expect(onChange).toHaveBeenLastCalledWith('23:07');
    fireEvent.change(minute, { target: { value: '75' } });
    expect(onChange).toHaveBeenLastCalledWith('23:59');
  });

  it('lets a field be cleared to retype, without telling the parent until it is a time again', () => {
    const { hour, onChange } = setup('06:30');
    fireEvent.change(hour, { target: { value: '' } });
    expect(hour).toHaveValue('');
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.change(hour, { target: { value: '9' } });
    expect(onChange).toHaveBeenLastCalledWith('09:30');
  });

  it('puts back the held value when a cleared part is left empty', () => {
    const { hour, onChange } = setup('06:30');
    fireEvent.change(hour, { target: { value: '' } });
    fireEvent.blur(hour);
    expect(hour).toHaveValue('06');
    expect(onChange).not.toHaveBeenCalled();
  });

  it('shows the zone the time is read on', () => {
    render(<TimeField id="t" value="06:00" zoneLabel="Sydney time" onChange={vi.fn()} />);
    expect(screen.getByText('Sydney time')).toBeInTheDocument();
  });
});
