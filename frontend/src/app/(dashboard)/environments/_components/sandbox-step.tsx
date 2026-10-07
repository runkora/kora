'use client';

/** @tier 3 — environments-scoped. The environment wizard's image choice and size card:
 * a summary of the selected image, a searchable paged image table, and size presets. */

import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  DataTable,
  Eyebrow,
  FormField,
  Grid,
  Icon,
  Input,
  RadioGroup,
  RadioGroupItem,
  Stack,
  Text,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import type { EnvironmentSizes, SandboxImage } from '@orbit/shared/api';
import { ExternalLink } from '@orbit/components/links';
import { BASE_IMAGE_NAME, imageProblem } from '@orbit/lib/environments';
import {
  PRESET_OPTIONS,
  SIZE_FIELDS,
  UNIT,
  customError,
  presetsFor,
  toAmount,
  sizeLine,
  withUnit,
  type Preset,
  type SizeChoice,
  type SizeKey,
} from './environment-size';

export const SANDBOX_INTRO =
  'Agents run commands, builds and tests inside the sandbox. The image decides which tools are installed.';

const WARM_BADGE = (
  <Tooltip>
    <TooltipTrigger asChild>
      <Badge variant="healthy" size="sm">
        Warm
      </Badge>
    </TooltipTrigger>
    <TooltipContent>Already downloaded, which makes sandboxes start quickly.</TooltipContent>
  </Tooltip>
);

/** The selected image's name, description, Warm badge and ref. */
export function ImageSummary({ image }: { image: SandboxImage | undefined }) {
  if (!image) return null;
  return (
    <Card subtle>
      <CardContent padded compact>
        <Stack direction="horizontal" align="center" gap="cozy">
          <Icon name="box" size="sm" color="accent" container containerVariant="accent" label="" />
          <Stack gap="none" grow minWidth="none">
            <Stack direction="horizontal" align="center" gap="snug">
              <Text as="span" size="supporting" weight="heavy">
                {image.name}
              </Text>
              {image.warm && WARM_BADGE}
            </Stack>
            <Text size="caption" color="muted">
              {image.description}
            </Text>
          </Stack>
          <Text as="span" size="micro" mono color="subtle" truncate>
            {image.image}
          </Text>
        </Stack>
      </CardContent>
    </Card>
  );
}

const COLUMNS: DataTableColumn<SandboxImage>[] = [
  {
    id: 'select',
    header: '',
    gridWidth: '24px',
    locked: true,
    cell: (image) => (
      <RadioGroupItem value={image.name} aria-label={image.name} disabled={!image.valid} />
    ),
  },
  {
    id: 'name',
    header: 'Name',
    gridWidth: 'minmax(0, 1.2fr)',
    wrap: true,
    locked: true,
    cell: (image) => (
      <Stack direction="horizontal" align="center" gap="tight" wrap>
        <Text
          as="span"
          size="supporting"
          weight="heavy"
          color={image.valid ? 'foreground' : 'subtle'}
        >
          {image.name}
        </Text>
        {image.warm && WARM_BADGE}
      </Stack>
    ),
  },
  {
    id: 'description',
    header: 'Description',
    gridWidth: 'minmax(0, 1.4fr)',
    wrap: true,
    locked: true,
    cell: (image) => (
      <Stack gap="none">
        <Text size="caption" color={image.valid ? 'muted' : 'subtle'}>
          {image.description}
        </Text>
        {!image.valid && (
          <Text size="caption" color="status-error">
            Not valid: {image.problem ?? 'unknown problem'}.
          </Text>
        )}
      </Stack>
    ),
  },
  {
    id: 'image',
    header: 'Image',
    gridWidth: 'minmax(0, 1.6fr)',
    locked: true,
    cell: (image) => (
      <Tooltip>
        <TooltipTrigger asChild>
          <Text as="span" size="micro" mono color="subtle" truncate>
            {image.image}
          </Text>
        </TooltipTrigger>
        <TooltipContent>
          {image.digest ? `${image.image}@${image.digest}` : image.image}
        </TooltipContent>
      </Tooltip>
    ),
  },
];

/** The searchable, paged image table with a radio per row. */
export function ImageTable({
  images,
  value,
  onChange,
}: {
  images: SandboxImage[];
  value: string;
  onChange: (name: string) => void;
}) {
  // The selected image always matches, so filtering never hides the current choice.
  const searchImages = (image: SandboxImage, query: string) =>
    image.name === value ||
    [image.name, image.description ?? '', image.image].some((v) =>
      v.toLowerCase().includes(query.toLowerCase()),
    );
  return (
    <RadioGroup value={value} onValueChange={onChange} aria-label="Sandbox image">
      <DataTable
        data={images}
        columns={COLUMNS}
        getRowKey={(image) => image.name}
        onRowClick={(image) => {
          if (image.valid) onChange(image.name);
        }}
        rowLabel={(image) => (image.valid ? `Use ${image.name}` : `${image.name} is not valid`)}
        emptyMessage="No sandbox images are registered."
        filteredEmptyMessage="No images match."
        toolbarTitle={<Eyebrow>Sandbox image</Eyebrow>}
        searchFn={searchImages}
        searchPlaceholder="Search images"
        noun="images"
        pagination={{ pageSizeOptions: [5, 10, 20], defaultPageSize: 5 }}
      />
    </RadioGroup>
  );
}

/** The image choice: the summary of the selected image, then the table, or a note when only the base image exists. */
export function ImageChoice({
  images,
  value,
  onChange,
}: {
  /** Registered sandbox images, or null when the list did not load. */
  images: SandboxImage[] | null;
  value: string;
  onChange: (name: string) => void;
}) {
  if (images === null) {
    return (
      <Text size="caption" color="muted">
        Could not load the registered images.
      </Text>
    );
  }
  const onlyBase = images.length === 1 && images[0]?.name === BASE_IMAGE_NAME;
  const selected = images.find((i) => i.name === value);
  return (
    <>
      <ImageSummary image={selected} />
      {!selected && (
        <Text size="caption" color="status-error">
          {value}: {imageProblem(value, images)}
        </Text>
      )}
      {onlyBase ? (
        <Text size="caption" color="muted">
          Only the base image is available. Platform teams can register more.{' '}
          <ExternalLink url="https://github.com/clear-route/orbit/blob/main/docs/guides/deployment.md#sandbox-images-and-environments">
            Read the sandbox image guide
          </ExternalLink>
        </Text>
      ) : (
        <ImageTable images={images} value={value} onChange={onChange} />
      )}
    </>
  );
}

/** The size card: preset cards, and number inputs under Custom. */
export function SizeCard({
  sizes,
  value,
  onChange,
}: {
  sizes: EnvironmentSizes;
  value: SizeChoice;
  onChange: (next: SizeChoice) => void;
}) {
  const presets = presetsFor(sizes);
  const max = (key: SizeKey) => Math.round(toAmount(key, sizes.max[key]) * 100) / 100;
  const setPreset = (preset: string) => onChange({ ...value, preset: preset as Preset });
  return (
    <Card subtle>
      <CardHeader
        compact
        divider
        leadingIcon="cpu"
        title="Size"
        description="How much CPU, memory and disk each sandbox gets."
      />
      <CardContent padded>
        <Stack gap="comfortable">
          <RadioGroup value={value.preset} onValueChange={setPreset} aria-label="Size preset">
            <Grid cols={{ base: 2, md: 4 }} gap="cozy">
              {PRESET_OPTIONS.map((option) => {
                const values = option.value === 'custom' ? undefined : presets[option.value];
                return (
                  <Card
                    key={option.value}
                    interactive
                    selected={value.preset === option.value}
                    onClick={() => setPreset(option.value)}
                    column
                  >
                    <CardContent padded compact grow>
                      <Stack gap="tight">
                        <Stack direction="horizontal" align="center" gap="snug">
                          <Text as="span" size="supporting" weight="heavy">
                            {option.label}
                          </Text>
                          {option.value === 'medium' && (
                            <Badge variant="secondary" size="sm">
                              Default
                            </Badge>
                          )}
                          <Stack grow />
                          <RadioGroupItem value={option.value} aria-label={option.label} />
                        </Stack>
                        <Text size="micro" color="muted">
                          {values ? sizeLine(values) : 'Choose your own values'}
                        </Text>
                      </Stack>
                    </CardContent>
                  </Card>
                );
              })}
            </Grid>
          </RadioGroup>
          {value.preset === 'custom' && (
            <Grid cols={{ base: 1, sm: 3 }} gap="cozy">
              {SIZE_FIELDS.map((field) => {
                const error = customError(value, sizes, field.key);
                return (
                  <FormField
                    key={field.key}
                    id={`size-${field.key}`}
                    label={field.label}
                    hint={`${field.purpose} Maximum ${withUnit(field.key, max(field.key))}.`}
                    error={error}
                  >
                    <Input
                      id={`size-${field.key}`}
                      type="number"
                      inputMode="decimal"
                      min={0}
                      max={max(field.key)}
                      step={field.key === 'cpu' ? 0.5 : 1}
                      value={value.custom[field.key]}
                      onChange={(e) =>
                        onChange({
                          ...value,
                          custom: { ...value.custom, [field.key]: e.target.value },
                        })
                      }
                      placeholder={String(
                        Math.round(toAmount(field.key, sizes.default[field.key]) * 100) / 100,
                      )}
                      hasError={error !== undefined}
                      endAdornment={UNIT[field.key]}
                    />
                  </FormField>
                );
              })}
            </Grid>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}
